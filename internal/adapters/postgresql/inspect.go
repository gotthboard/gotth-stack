package postgresql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type imageInspect struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		Env        []string `json:"Env"`
		Entrypoint []string `json:"Entrypoint"`
		Cmd        []string `json:"Cmd"`
	} `json:"Config"`
}

type containerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Image      string            `json:"Image"`
		User       string            `json:"User"`
		Env        []string          `json:"Env"`
		Labels     map[string]string `json:"Labels"`
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
	} `json:"Config"`
	HostConfig struct {
		ReadonlyRootfs bool                 `json:"ReadonlyRootfs"`
		Privileged     bool                 `json:"Privileged"`
		AutoRemove     bool                 `json:"AutoRemove"`
		CapAdd         []string             `json:"CapAdd"`
		CapDrop        []string             `json:"CapDrop"`
		SecurityOpt    []string             `json:"SecurityOpt"`
		Tmpfs          map[string]string    `json:"Tmpfs"`
		PortBindings   map[string][]portMap `json:"PortBindings"`
		RestartPolicy  struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

type portMap struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func decodeOne(value []byte, target any) error {
	if len(value) == 0 {
		return ErrEngine
	}
	if len(value) > MaxOutputBytes {
		return ErrLimit
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return ErrEngine
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrEngine
	}
	return nil
}

func (adapter *Adapter) engineDigest(ctx context.Context) (string, error) {
	value, err := adapter.run(ctx, commandRequest{kind: commandVersion}, true)
	if err != nil {
		return "", err
	}
	var decoded map[string]any
	if err := decodeOne(value, &decoded); err != nil || decoded["Version"] == nil || decoded["ApiVersion"] == nil || decoded["Os"] != "linux" || decoded["Arch"] == nil {
		return "", ErrEngine
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", ErrEngine
	}
	return digest(canonical), nil
}

func (adapter *Adapter) inspectImage(ctx context.Context, spec managedSpec) (string, error) {
	value, err := adapter.run(ctx, commandRequest{kind: commandImageInspect, spec: spec}, true)
	if err != nil {
		if errors.Is(err, ErrExecutable) || errors.Is(err, ErrLimit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", ErrImage
	}
	var records []imageInspect
	if err := decodeOne(value, &records); err != nil || len(records) != 1 {
		return "", ErrImage
	}
	record := records[0]
	if !digestPattern.MatchString(record.ID) || !containsExact(record.RepoDigests, spec.Specification.Image) || len(record.Config.Entrypoint) != 1 || record.Config.Entrypoint[0] != "docker-entrypoint.sh" || len(record.Config.Cmd) != 1 || record.Config.Cmd[0] != "postgres" {
		return "", ErrImage
	}
	major, ok := uniqueEnvironment(record.Config.Env, "PG_MAJOR")
	if !ok || major != "17" {
		return "", ErrImage
	}
	return record.ID, nil
}

func (adapter *Adapter) inspectContainer(ctx context.Context, name string) (*containerRecord, error) {
	presence, err := adapter.run(ctx, commandRequest{kind: commandContainerList, name: name}, true)
	if err != nil {
		return nil, err
	}
	switch string(presence) {
	case "":
		return nil, nil
	case name + "\n":
	default:
		return nil, ErrContainer
	}
	value, err := adapter.run(ctx, commandRequest{kind: commandContainerInspect, name: name}, true)
	if err != nil {
		return nil, err
	}
	var records []containerInspect
	if err := decodeOne(value, &records); err != nil || len(records) != 1 {
		return nil, ErrContainer
	}
	return adapter.normalizeContainer(name, records[0])
}

func (adapter *Adapter) normalizeContainer(actualName string, record containerInspect) (*containerRecord, error) {
	if record.Name != "/"+actualName || !hexIDPattern.MatchString(record.ID) || !digestPattern.MatchString(record.Image) {
		return nil, ErrContainer
	}
	labels := record.Config.Labels
	component := labels[labelComponent]
	configuration := labels[labelConfiguration]
	secretRevision := labels[labelSecretRevision]
	operationID := labels[labelOperation]
	if labels[labelAdapter] != adapterLabelValue || !validID(component) || !validID(operationID) || !digestPattern.MatchString(configuration) || !digestPattern.MatchString(secretRevision) {
		return nil, ErrContainer
	}
	allowedLabels := map[string]bool{labelAdapter: true, labelComponent: true, labelConfiguration: true, labelSecretRevision: true, labelOperation: true}
	for key := range labels {
		if strings.HasPrefix(key, "com.gotthstack.") && !allowedLabels[key] {
			return nil, ErrContainer
		}
	}
	primaryName := adapter.containerName(component)
	if actualName != primaryName && !strings.HasPrefix(actualName, primaryName+"-rollback-") {
		return nil, ErrContainer
	}
	uid, gid, ok := parseUser(record.Config.User)
	if !ok {
		return nil, ErrContainer
	}
	database, okDatabase := uniqueEnvironment(record.Config.Env, "POSTGRES_DB")
	role, okRole := uniqueEnvironment(record.Config.Env, "POSTGRES_USER")
	passwordFile, okPassword := uniqueEnvironment(record.Config.Env, "POSTGRES_PASSWORD_FILE")
	if !okDatabase || !okRole || !okPassword || passwordFile != secretTarget || !validID(database) || !validID(role) {
		return nil, ErrContainer
	}
	for _, value := range record.Config.Env {
		key, _, ok := strings.Cut(value, "=")
		if ok && strings.HasPrefix(key, "POSTGRES_") && key != "POSTGRES_DB" && key != "POSTGRES_USER" && key != "POSTGRES_PASSWORD_FILE" {
			return nil, ErrContainer
		}
	}
	port, ok := exactPort(record.HostConfig.PortBindings)
	if !ok {
		return nil, ErrContainer
	}
	specification := Specification{Image: record.Config.Image, Database: database, Role: role, Port: port, UID: uid, GID: gid}
	computed, err := configurationDigest(specification)
	if err != nil || computed != configuration {
		return nil, ErrContainer
	}
	managed := managedSpec{
		OperationID: operationID, ComponentID: component, ConfigurationDigest: configuration,
		SecretRevision: secretRevision, Specification: specification, ImageID: record.Image,
		ContainerName: primaryName, DataName: component, SecretName: component + "-password",
	}
	if !adapter.validEffectiveContainer(managed, record) {
		return nil, ErrContainer
	}
	return &containerRecord{spec: managed, running: record.State.Running, id: record.ID}, nil
}

func (adapter *Adapter) validEffectiveContainer(spec managedSpec, record containerInspect) bool {
	if !record.HostConfig.ReadonlyRootfs || record.HostConfig.Privileged || record.HostConfig.AutoRemove || len(record.HostConfig.CapAdd) != 0 || !sameStringsFold(record.HostConfig.CapDrop, []string{"ALL"}) || !sameStrings(record.HostConfig.SecurityOpt, []string{"no-new-privileges"}) || record.HostConfig.RestartPolicy.Name != "no" && record.HostConfig.RestartPolicy.Name != "" {
		return false
	}
	if len(record.Config.Entrypoint) != 1 || record.Config.Entrypoint[0] != "docker-entrypoint.sh" || len(record.Config.Cmd) != 1 || record.Config.Cmd[0] != "postgres" {
		return false
	}
	if !tmpfsMatches(record.HostConfig.Tmpfs, "/tmp", []string{"rw", "noexec", "nosuid", "nodev", "size=67108864"}) || !tmpfsMatches(record.HostConfig.Tmpfs, "/var/run/postgresql", []string{"rw", "noexec", "nosuid", "nodev", "size=16777216"}) || len(record.HostConfig.Tmpfs) != 2 {
		return false
	}
	if len(record.Mounts) != 2 {
		return false
	}
	wanted := map[string]struct {
		source string
		rw     bool
	}{
		dataTarget:   {source: filepath.Join(adapter.dataPath, spec.DataName), rw: true},
		secretTarget: {source: filepath.Join(adapter.secretPath, spec.SecretName), rw: false},
	}
	for _, mount := range record.Mounts {
		expected, ok := wanted[mount.Destination]
		if !ok || mount.Type != "bind" || mount.Source != expected.source || mount.RW != expected.rw {
			return false
		}
		delete(wanted, mount.Destination)
	}
	return len(wanted) == 0
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func uniqueEnvironment(values []string, key string) (string, bool) {
	prefix := key + "="
	var found string
	seen := false
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			if seen {
				return "", false
			}
			found, seen = strings.TrimPrefix(value, prefix), true
		}
	}
	return found, seen
}

func parseUser(value string) (uint32, uint32, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	uid, uidErr := strconv.ParseUint(parts[0], 10, 32)
	gid, gidErr := strconv.ParseUint(parts[1], 10, 32)
	return uint32(uid), uint32(gid), uidErr == nil && gidErr == nil && uid > 0 && gid > 0
}

func exactPort(values map[string][]portMap) (uint16, bool) {
	if len(values) != 1 {
		return 0, false
	}
	mappings, ok := values["5432/tcp"]
	if !ok || len(mappings) != 1 || mappings[0].HostIP != "127.0.0.1" {
		return 0, false
	}
	port, err := strconv.ParseUint(mappings[0].HostPort, 10, 16)
	return uint16(port), err == nil && port > 0
}

func tmpfsMatches(values map[string]string, path string, wanted []string) bool {
	actual, ok := values[path]
	if !ok {
		return false
	}
	parts := strings.Split(actual, ",")
	sort.Strings(parts)
	wantedCopy := append([]string(nil), wanted...)
	sort.Strings(wantedCopy)
	return sameStrings(parts, wantedCopy)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameStringsFold(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !strings.EqualFold(left[index], right[index]) {
			return false
		}
	}
	return true
}
