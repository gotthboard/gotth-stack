package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

type imageInspect struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		User       string            `json:"User"`
		Env        []string          `json:"Env"`
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
		Labels     map[string]string `json:"Labels"`
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
		ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
		Privileged     bool              `json:"Privileged"`
		AutoRemove     bool              `json:"AutoRemove"`
		CapAdd         []string          `json:"CapAdd"`
		CapDrop        []string          `json:"CapDrop"`
		SecurityOpt    []string          `json:"SecurityOpt"`
		Tmpfs          map[string]string `json:"Tmpfs"`
		PortBindings   map[string]any    `json:"PortBindings"`
		NetworkMode    string            `json:"NetworkMode"`
		ShmSize        int64             `json:"ShmSize"`
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

func decodeOne(value []byte, target any) error {
	if len(value) == 0 || len(value) > MaxOutputBytes {
		return ErrEngine
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
	if !digestPattern.MatchString(record.ID) || !containsExact(record.RepoDigests, spec.Specification.Image) || record.Config.User != "1000" || len(record.Config.Entrypoint) != 3 || record.Config.Entrypoint[0] != "dumb-init" || record.Config.Entrypoint[1] != "--" || record.Config.Entrypoint[2] != "ak" || len(record.Config.Cmd) != 0 || !versionPattern.MatchString(record.Config.Labels["org.opencontainers.image.version"]) {
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
	role := Role(labels[labelRole])
	component := labels[labelComponent]
	operation := labels[labelOperation]
	configuration := labels[labelConfiguration]
	databaseSecret := labels[labelDatabaseSecret]
	keySecret := labels[labelKeySecret]
	if labels[labelAdapter] != adapterLabelValue || !validRole(role) || !validID(component) || !validID(operation) || !digestPattern.MatchString(configuration) || !digestPattern.MatchString(databaseSecret) || !digestPattern.MatchString(keySecret) {
		return nil, ErrContainer
	}
	allowedLabels := map[string]bool{labelAdapter: true, labelComponent: true, labelConfiguration: true, labelDatabaseSecret: true, labelKeySecret: true, labelOperation: true, labelRole: true}
	for key := range labels {
		if strings.HasPrefix(key, "com.gotthstack.") && !allowedLabels[key] {
			return nil, ErrContainer
		}
	}
	primary := adapter.containerName(component, role)
	if actualName != primary && !strings.HasPrefix(actualName, primary+"-rollback-") {
		return nil, ErrContainer
	}
	uid, gid, ok := parseUser(record.Config.User)
	if !ok || uid == 0 || gid == 0 {
		return nil, ErrContainer
	}
	env, ok := relevantEnvironment(record.Config.Env)
	if !ok {
		return nil, ErrContainer
	}
	port := func(key string) (uint16, bool) {
		value, found := env[key]
		if !found {
			return 0, false
		}
		parsed, err := strconv.ParseUint(value, 10, 16)
		return uint16(parsed), err == nil
	}
	databasePort, okDBPort := port("AUTHENTIK_POSTGRESQL__PORT")
	httpPort, okHTTP := loopbackPort(env["AUTHENTIK_LISTEN__HTTP"])
	httpsPort, okHTTPS := loopbackPort(env["AUTHENTIK_LISTEN__HTTPS"])
	metricsPort, okMetrics := loopbackPort(env["AUTHENTIK_LISTEN__METRICS"])
	if !okDBPort || !okHTTP || !okHTTPS || !okMetrics {
		return nil, ErrContainer
	}
	spec := Specification{Image: record.Config.Image, DatabaseHost: env["AUTHENTIK_POSTGRESQL__HOST"], DatabasePort: databasePort, Database: env["AUTHENTIK_POSTGRESQL__NAME"], DatabaseRole: env["AUTHENTIK_POSTGRESQL__USER"], HTTPPort: httpPort, HTTPSPort: httpsPort, UID: uid, GID: gid}
	if role == RoleServer {
		spec.ServerMetricsPort = metricsPort
	} else {
		spec.WorkerHTTPPort = httpPort
		spec.WorkerHTTPSPort = httpsPort
		spec.HTTPPort = 0
		spec.HTTPSPort = 0
		spec.WorkerMetricsPort = metricsPort
	}
	if !adapter.validEffectiveContainer(component, role, record) {
		return nil, ErrContainer
	}
	return &containerRecord{operationID: operation, componentID: component, configurationDigest: configuration, databaseSecret: databaseSecret, keySecret: keySecret, image: record.Config.Image, imageID: record.Image, role: role, specification: spec, running: record.State.Running, id: record.ID}, nil
}

func relevantEnvironment(values []string) (map[string]string, bool) {
	wanted := map[string]string{
		"AUTHENTIK_POSTGRESQL__HOST": "", "AUTHENTIK_POSTGRESQL__PORT": "", "AUTHENTIK_POSTGRESQL__NAME": "", "AUTHENTIK_POSTGRESQL__USER": "", "AUTHENTIK_POSTGRESQL__PASSWORD": "",
		"AUTHENTIK_POSTGRESQL__SSLMODE": "", "AUTHENTIK_SECRET_KEY": "", "AUTHENTIK_ERROR_REPORTING__ENABLED": "", "AUTHENTIK_DISABLE_UPDATE_CHECK": "", "AUTHENTIK_LISTEN__HTTP": "", "AUTHENTIK_LISTEN__HTTPS": "", "AUTHENTIK_LISTEN__METRICS": "",
	}
	seen := make(map[string]string, len(wanted))
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return nil, false
		}
		_, expected := wanted[key]
		if strings.HasPrefix(key, "AUTHENTIK_") || strings.HasPrefix(key, "POSTGRES_") {
			if !expected {
				return nil, false
			}
			if _, duplicate := seen[key]; duplicate {
				return nil, false
			}
			seen[key] = value
		}
	}
	if len(seen) != len(wanted) || seen["AUTHENTIK_POSTGRESQL__PASSWORD"] != "file://"+databaseSecretTarget || seen["AUTHENTIK_SECRET_KEY"] != "file://"+keySecretTarget || seen["AUTHENTIK_POSTGRESQL__SSLMODE"] != "disable" || seen["AUTHENTIK_ERROR_REPORTING__ENABLED"] != "false" || seen["AUTHENTIK_DISABLE_UPDATE_CHECK"] != "true" {
		return nil, false
	}
	return seen, true
}

func (adapter *Adapter) validEffectiveContainer(component string, role Role, record containerInspect) bool {
	if !record.HostConfig.ReadonlyRootfs || record.HostConfig.Privileged || record.HostConfig.AutoRemove || len(record.HostConfig.CapAdd) != 0 || !sameStringsFold(record.HostConfig.CapDrop, []string{"ALL"}) || !sameStrings(record.HostConfig.SecurityOpt, []string{"no-new-privileges"}) || record.HostConfig.NetworkMode != "host" || record.HostConfig.ShmSize != 536870912 || len(record.HostConfig.PortBindings) != 0 || record.HostConfig.RestartPolicy.Name != "" && record.HostConfig.RestartPolicy.Name != "no" {
		return false
	}
	if len(record.Config.Entrypoint) != 3 || record.Config.Entrypoint[0] != "dumb-init" || record.Config.Entrypoint[1] != "--" || record.Config.Entrypoint[2] != "ak" || len(record.Config.Cmd) != 1 || record.Config.Cmd[0] != string(role) {
		return false
	}
	if !tmpfsMatches(record.HostConfig.Tmpfs, "/tmp", []string{"rw", "noexec", "nosuid", "nodev", "size=67108864"}) || len(record.HostConfig.Tmpfs) != 1 {
		return false
	}
	wanted := map[string]struct {
		source string
		rw     bool
	}{
		dataTarget:           {filepath.Join(adapter.dataPath, component, "data"), true},
		templatesTarget:      {filepath.Join(adapter.dataPath, component, "templates"), false},
		databaseSecretTarget: {filepath.Join(adapter.secretPath, component+"-postgresql-password"), false},
		keySecretTarget:      {filepath.Join(adapter.secretPath, component+"-secret-key"), false},
	}
	if role == RoleWorker {
		wanted[certsTarget] = struct {
			source string
			rw     bool
		}{filepath.Join(adapter.dataPath, component, "certs"), true}
	}
	if len(record.Mounts) != len(wanted) {
		return false
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

func managedFromPair(server, worker *containerRecord) (managedSpec, error) {
	if server == nil || worker == nil || server.role != RoleServer || worker.role != RoleWorker || server.operationID != worker.operationID || server.componentID != worker.componentID || server.configurationDigest != worker.configurationDigest || server.databaseSecret != worker.databaseSecret || server.keySecret != worker.keySecret || server.image != worker.image || server.imageID != worker.imageID {
		return managedSpec{}, ErrContainer
	}
	a, b := server.specification, worker.specification
	if a.Image != b.Image || a.DatabaseHost != b.DatabaseHost || a.DatabasePort != b.DatabasePort || a.Database != b.Database || a.DatabaseRole != b.DatabaseRole || a.UID != b.UID || a.GID != b.GID {
		return managedSpec{}, ErrContainer
	}
	a.WorkerHTTPPort = b.WorkerHTTPPort
	a.WorkerHTTPSPort = b.WorkerHTTPSPort
	a.WorkerMetricsPort = b.WorkerMetricsPort
	computed, err := configurationDigest(a)
	if err != nil || computed != server.configurationDigest {
		return managedSpec{}, ErrContainer
	}
	return managedSpec{OperationID: server.operationID, ComponentID: server.componentID, ConfigurationDigest: server.configurationDigest, DatabaseSecretRevision: server.databaseSecret, KeySecretRevision: server.keySecret, Specification: a, ImageID: server.imageID}, nil
}

func recordMatches(record *containerRecord, spec managedSpec, role Role) bool {
	if record == nil || record.role != role || record.operationID != spec.OperationID || record.componentID != spec.ComponentID || record.configurationDigest != spec.ConfigurationDigest || record.databaseSecret != spec.DatabaseSecretRevision || record.keySecret != spec.KeySecretRevision || record.image != spec.Specification.Image || record.imageID != spec.ImageID {
		return false
	}
	a, b := record.specification, spec.Specification
	if a.DatabaseHost != b.DatabaseHost || a.DatabasePort != b.DatabasePort || a.Database != b.Database || a.DatabaseRole != b.DatabaseRole || a.UID != b.UID || a.GID != b.GID {
		return false
	}
	if role == RoleServer {
		return a.HTTPPort == b.HTTPPort && a.HTTPSPort == b.HTTPSPort && a.ServerMetricsPort == b.ServerMetricsPort
	}
	return a.WorkerHTTPPort == b.WorkerHTTPPort && a.WorkerHTTPSPort == b.WorkerHTTPSPort && a.WorkerMetricsPort == b.WorkerMetricsPort
}

func loopbackPort(value string) (uint16, bool) {
	const prefix = "127.0.0.1:"
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 16)
	return uint16(parsed), err == nil && parsed >= 1024
}

func parseUser(value string) (uint32, uint32, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	uid, e1 := strconv.ParseUint(parts[0], 10, 32)
	gid, e2 := strconv.ParseUint(parts[1], 10, 32)
	return uint32(uid), uint32(gid), e1 == nil && e2 == nil
}

func containsExact(values []string, wanted string) bool {
	for _, v := range values {
		if v == wanted {
			return true
		}
	}
	return false
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameStringsFold(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		found := false
		for i, y := range b {
			if !used[i] && strings.EqualFold(x, y) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func tmpfsMatches(values map[string]string, path string, wanted []string) bool {
	raw, ok := values[path]
	if !ok {
		return false
	}
	got := strings.Split(raw, ",")
	return sameStringSet(got, wanted)
}
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
		if m[v] < 0 {
			return false
		}
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
