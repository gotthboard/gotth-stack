package mailruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type imageInspect struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		User       string            `json:"User"`
		Env        []string          `json:"Env"`
		Labels     map[string]string `json:"Labels"`
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
	} `json:"Config"`
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
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
		ReadonlyRootfs bool                     `json:"ReadonlyRootfs"`
		Privileged     bool                     `json:"Privileged"`
		AutoRemove     bool                     `json:"AutoRemove"`
		NetworkMode    string                   `json:"NetworkMode"`
		CapAdd         []string                 `json:"CapAdd"`
		CapDrop        []string                 `json:"CapDrop"`
		SecurityOpt    []string                 `json:"SecurityOpt"`
		Tmpfs          map[string]string        `json:"Tmpfs"`
		PortBindings   map[string][]portBinding `json:"PortBindings"`
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
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases []string `json:"Aliases"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

type networkInspect struct {
	Name       string            `json:"Name"`
	ID         string            `json:"Id"`
	Created    string            `json:"Created"`
	Scope      string            `json:"Scope"`
	Driver     string            `json:"Driver"`
	EnableIPv6 bool              `json:"EnableIPv6"`
	Internal   bool              `json:"Internal"`
	Attachable bool              `json:"Attachable"`
	Ingress    bool              `json:"Ingress"`
	Labels     map[string]string `json:"Labels"`
	IPAM       struct {
		Driver  string `json:"Driver"`
		Options any    `json:"Options"`
		Config  []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
	Options map[string]string `json:"Options"`
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

func (adapter *adapter) engineDigest(ctx context.Context) (string, error) {
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

func (adapter *adapter) inspectNetwork(ctx context.Context) (NetworkSummary, bool, error) {
	presence, err := adapter.run(ctx, commandRequest{kind: commandNetworkList, name: adapter.network}, true)
	if err != nil {
		return NetworkSummary{}, false, err
	}
	switch string(presence) {
	case "":
		return NetworkSummary{}, false, nil
	case adapter.network + "\n":
	default:
		return NetworkSummary{}, false, ErrEngine
	}
	value, err := adapter.run(ctx, commandRequest{kind: commandNetworkInspect, name: adapter.network}, true)
	if err != nil {
		return NetworkSummary{}, false, err
	}
	var records []networkInspect
	if err := decodeOne(value, &records); err != nil || len(records) != 1 {
		return NetworkSummary{}, false, ErrEngine
	}
	record := records[0]
	if record.Name != adapter.network || !containerIDPattern.MatchString(record.ID) || record.Scope != "local" || record.Driver != "bridge" || record.Internal || record.Attachable || record.Ingress || record.EnableIPv6 || record.IPAM.Driver != "default" || len(record.IPAM.Config) != 1 || record.IPAM.Config[0].Subnet == "" || record.IPAM.Config[0].Gateway == "" || record.Labels["com.gotthstack.network"] != "mailruntime.v1" {
		return NetworkSummary{}, false, ErrEngine
	}
	for key := range record.Labels {
		if strings.HasPrefix(key, "com.gotthstack.") && key != "com.gotthstack.network" {
			return NetworkSummary{}, false, ErrEngine
		}
	}
	canonical, err := json.Marshal(record)
	if err != nil {
		return NetworkSummary{}, false, ErrEngine
	}
	return NetworkSummary{Name: record.Name, ID: record.ID, Digest: digest(canonical)}, true, nil
}

func (adapter *adapter) inspectImage(ctx context.Context, spec managedSpec) (string, error) {
	value, err := adapter.run(ctx, commandRequest{kind: commandImageInspect, spec: spec, definition: adapter.definition}, true)
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
	if !hexIDPattern.MatchString(record.ID) || !containsExact(record.RepoDigests, spec.Candidate.Image) || record.Config.User != adapter.definition.user || !reflect.DeepEqual(record.Config.Entrypoint, []string{"/usr/local/bin/gotth-mail-entrypoint"}) || len(record.Config.Cmd) != 0 || record.Config.Labels[imageLabelRole] != string(spec.Role) || record.Config.Labels[imageLabelSource] != spec.Candidate.SourceCommit || record.Config.Labels[imageLabelVersion] != spec.Candidate.ProductVersion {
		return "", ErrImage
	}
	for _, value := range record.Config.Env {
		key, _, _ := strings.Cut(value, "=")
		if strings.Contains(key, "PASSWORD") || strings.Contains(key, "SECRET") || strings.Contains(key, "TOKEN") || strings.HasPrefix(key, "GOTTH_MAIL_") {
			return "", ErrImage
		}
	}
	return record.ID, nil
}

func (adapter *adapter) inspectContainer(ctx context.Context, name string, expected *managedSpec) (*containerRecord, error) {
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
	return adapter.normalizeContainer(name, records[0], expected)
}

func (adapter *adapter) discoverContainer(ctx context.Context, name string, configurationMembers []FileArtifact) (*containerRecord, error) {
	presence, err := adapter.run(ctx, commandRequest{kind: commandContainerList, name: name}, true)
	if err != nil {
		return nil, err
	}
	if string(presence) == "" {
		return nil, nil
	}
	if string(presence) != name+"\n" {
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
	record := records[0]
	labels := record.Config.Labels
	candidate := Candidate{
		OperationID: labels[labelOperation], ComponentID: labels[labelComponent], ProductVersion: labels[labelVersion],
		SourceCommit: labels[labelSourceCommit], Image: record.Config.Image, ConfigurationDigest: labels[labelConfiguration],
		SecretRevisionDigest: labels[labelSecretSet],
	}
	candidate.ConfigurationSize, err = strconv.ParseInt(labels[labelConfigSize], 10, 64)
	if err != nil {
		return nil, ErrContainer
	}
	candidate.ConfigurationMembers = append([]FileArtifact(nil), configurationMembers...)
	mountSources, ok := adapter.effectiveMountSources(record, candidate.ConfigurationDigest)
	if !ok {
		return nil, ErrContainer
	}
	spec := managedSpec{Candidate: candidate, Role: adapter.definition.role, ImageID: record.Image, ContainerName: adapter.containerName(candidate.ComponentID), NetworkName: adapter.network, MountSources: mountSources}
	if !validCandidate(candidate, adapter.definition) {
		return nil, ErrContainer
	}
	imageID, err := adapter.inspectImage(ctx, spec)
	if err != nil || imageID != spec.ImageID {
		return nil, ErrImage
	}
	return adapter.normalizeContainer(name, record, &spec)
}

func (adapter *adapter) normalizeContainer(actualName string, record containerInspect, expected *managedSpec) (*containerRecord, error) {
	if expected == nil || record.Name != "/"+actualName || !containerIDPattern.MatchString(record.ID) || !hexIDPattern.MatchString(record.Image) || record.Config.Image != expected.Candidate.Image || record.Image != expected.ImageID {
		return nil, ErrContainer
	}
	labels := record.Config.Labels
	wantedLabels := map[string]string{
		labelAdapter: adapter.definition.adapterID, labelComponent: expected.Candidate.ComponentID,
		labelConfiguration: expected.Candidate.ConfigurationDigest, labelSecretSet: expected.Candidate.SecretRevisionDigest,
		labelConfigSize: strconv.FormatInt(expected.Candidate.ConfigurationSize, 10),
		labelOperation:  expected.Candidate.OperationID, labelVersion: expected.Candidate.ProductVersion, labelSourceCommit: expected.Candidate.SourceCommit,
	}
	for key, wanted := range wantedLabels {
		if labels[key] != wanted {
			return nil, ErrContainer
		}
	}
	for key := range labels {
		if strings.HasPrefix(key, "com.gotthstack.") {
			if _, ok := wantedLabels[key]; !ok {
				return nil, ErrContainer
			}
		}
	}
	if !adapter.validEffectiveContainer(record, *expected) {
		return nil, ErrContainer
	}
	return &containerRecord{spec: *expected, running: record.State.Running, id: record.ID}, nil
}

func (adapter *adapter) validEffectiveContainer(record containerInspect, expected managedSpec) bool {
	definition := adapter.definition
	if !record.HostConfig.ReadonlyRootfs || record.HostConfig.Privileged || record.HostConfig.AutoRemove || record.HostConfig.NetworkMode != adapter.network || !sameStringsFold(record.HostConfig.CapAdd, definition.capAdd) || !sameStringsFold(record.HostConfig.CapDrop, []string{"ALL"}) || !sameStrings(record.HostConfig.SecurityOpt, []string{"no-new-privileges"}) || record.HostConfig.RestartPolicy.Name != "" && record.HostConfig.RestartPolicy.Name != "no" || record.Config.User != definition.user || !reflect.DeepEqual(record.Config.Entrypoint, []string{"/usr/local/bin/gotth-mail-entrypoint"}) || len(record.Config.Cmd) != 0 {
		return false
	}
	config, ok := uniqueEnvironment(record.Config.Env, "GOTTH_MAIL_CONFIG_DIR")
	if !ok || config != configTarget {
		return false
	}
	for _, value := range record.Config.Env {
		key, _, _ := strings.Cut(value, "=")
		if strings.Contains(key, "PASSWORD") || strings.Contains(key, "SECRET") || strings.Contains(key, "TOKEN") || strings.HasPrefix(key, "GOTTH_MAIL_") && key != "GOTTH_MAIL_CONFIG_DIR" {
			return false
		}
	}
	if !tmpfsMatches(record.HostConfig.Tmpfs, "/tmp", []string{"rw", "noexec", "nosuid", "nodev", "size=67108864"}) || !tmpfsMatches(record.HostConfig.Tmpfs, "/run", []string{"rw", "noexec", "nosuid", "nodev", "size=16777216"}) || len(record.HostConfig.Tmpfs) != 2 {
		return false
	}
	if len(record.Mounts) != len(definition.mounts) || len(expected.MountSources) != len(definition.mounts) {
		return false
	}
	wantedMounts := make(map[string]mountSpec, len(definition.mounts))
	for index, mount := range definition.mounts {
		mount.source = expected.MountSources[index]
		wantedMounts[mount.destination] = mount
	}
	for _, mount := range record.Mounts {
		wanted, ok := wantedMounts[mount.Destination]
		if !ok || mount.Type != "bind" || mount.Source != wanted.source || mount.RW == wanted.readOnly {
			return false
		}
		delete(wantedMounts, mount.Destination)
	}
	if len(wantedMounts) != 0 || !exactPorts(record.HostConfig.PortBindings, definition.ports) {
		return false
	}
	if len(record.NetworkSettings.Networks) != 1 {
		return false
	}
	network, ok := record.NetworkSettings.Networks[adapter.network]
	return ok && containsExact(network.Aliases, definition.alias)
}

func (adapter *adapter) effectiveMountSources(record containerInspect, configurationDigest string) ([]string, bool) {
	if len(record.Mounts) != len(adapter.definition.mounts) || !digestPattern.MatchString(configurationDigest) {
		return nil, false
	}
	actual := make(map[string]string, len(record.Mounts))
	for _, mount := range record.Mounts {
		if mount.Type != "bind" {
			return nil, false
		}
		actual[mount.Destination] = mount.Source
	}
	result := make([]string, len(adapter.definition.mounts))
	for index, mount := range adapter.definition.mounts {
		source, ok := actual[mount.destination]
		if !ok {
			return nil, false
		}
		wanted := adapter.expectedMountSource(index, configurationDigest, labelsSecretDigest(record.Config.Labels))
		if source != wanted {
			return nil, false
		}
		result[index] = source
	}
	return result, true
}

func labelsSecretDigest(labels map[string]string) string { return labels[labelSecretSet] }

func exactPorts(actual map[string][]portBinding, expected []portSpec) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, port := range expected {
		key := strconv.Itoa(int(port.containerPort)) + "/tcp"
		bindings, ok := actual[key]
		if !ok || len(bindings) != 1 || bindings[0].HostIP != port.hostIP || bindings[0].HostPort != strconv.Itoa(int(port.hostPort)) {
			return false
		}
	}
	return true
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
	found := ""
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
	return reflect.DeepEqual(left, right)
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
