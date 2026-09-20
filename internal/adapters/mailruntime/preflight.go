package mailruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

func (value *ControlPlane) Preflight(ctx context.Context, request ControlPlaneRequest) (*Prepared, Summary, error) {
	return value.adapter.preflight(ctx, request.Candidate)
}

func (value *Front) Preflight(ctx context.Context, request FrontRequest) (*Prepared, Summary, error) {
	return value.adapter.preflight(ctx, request.Candidate)
}

func (value *Postfix) Preflight(ctx context.Context, request PostfixRequest) (*Prepared, Summary, error) {
	return value.adapter.preflight(ctx, request.Candidate)
}

func (value *Dovecot) Preflight(ctx context.Context, request DovecotRequest) (*Prepared, Summary, error) {
	return value.adapter.preflight(ctx, request.Candidate)
}

func (value *Rspamd) Preflight(ctx context.Context, request RspamdRequest) (*Prepared, Summary, error) {
	return value.adapter.preflight(ctx, request.Candidate)
}

func (adapter *adapter) preflight(ctx context.Context, candidate Candidate) (*Prepared, Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil, Summary{}, ErrClosed
	}
	if !validCandidate(candidate, adapter.definition) || len(adapter.containerName(candidate.ComponentID)) > 63 {
		return nil, Summary{}, ErrInvalidInput
	}
	if err := adapter.validateCandidateMountPaths(candidate); err != nil {
		return nil, Summary{}, err
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	defer cancel()
	network, exists, err := adapter.inspectNetwork(bounded)
	if err != nil {
		return nil, Summary{}, err
	} else if !exists {
		return nil, Summary{}, ErrConflict
	}
	if err := adapter.verifyConfiguration(candidate.ConfigurationMembers, candidate.ConfigurationDigest, candidate.ConfigurationSize); err != nil {
		return nil, Summary{}, err
	}
	revision, err := adapter.revisionDigest()
	if err != nil || revision != candidate.SecretRevisionDigest {
		return nil, Summary{}, ErrSecret
	}
	mounts, err := adapter.captureMountIdentities()
	if err != nil {
		return nil, Summary{}, err
	}
	spec := managedSpec{Candidate: candidate, Role: adapter.definition.role, ContainerName: adapter.containerName(candidate.ComponentID), NetworkName: adapter.network, MountSources: adapter.candidateMountSources()}
	spec.ImageID, err = adapter.inspectImage(bounded, spec)
	if err != nil {
		return nil, Summary{}, err
	}
	engine, err := adapter.engineDigest(bounded)
	if err != nil {
		return nil, Summary{}, err
	}
	rollbackName := adapter.rollbackName(spec.ContainerName, candidate.OperationID)
	rollback, err := adapter.discoverContainer(bounded, rollbackName, candidate.ConfigurationMembers)
	if err != nil {
		return nil, Summary{}, err
	}
	if rollback != nil {
		return nil, Summary{}, ErrRecoveryRequired
	}
	primary, err := adapter.discoverContainer(bounded, spec.ContainerName, candidate.ConfigurationMembers)
	if err != nil {
		return nil, Summary{}, err
	}
	var previous *managedSpec
	previousRunning := false
	if primary != nil {
		if primary.spec.Candidate.ComponentID != candidate.ComponentID || primary.spec.Role != adapter.definition.role || primary.spec.NetworkName != adapter.network {
			return nil, Summary{}, ErrConflict
		}
		copy := primary.spec
		previous = &copy
		previousRunning = primary.running
	}
	metadata := transactionMetadata{
		SchemaVersion: RuntimeSchemaVersion, Role: adapter.definition.role, OperationID: candidate.OperationID,
		ComponentID: candidate.ComponentID, BindingDigest: adapter.bindingDigest(), NetworkDigest: network.Digest, EngineDigest: engine,
		Mounts: mounts, Candidate: spec, Previous: previous, PreviousRunning: previousRunning,
	}
	unsigned, err := json.Marshal(metadata)
	if err != nil {
		return nil, Summary{}, ErrStorage
	}
	metadata.RollbackReference = digest(unsigned)
	prepared := &Prepared{adapter: adapter, metadata: metadata}
	return prepared, summary(metadata), nil
}

func (adapter *adapter) candidateMountSources() []string {
	result := make([]string, 0, len(adapter.definition.mounts))
	for _, mount := range adapter.definition.mounts {
		result = append(result, mount.source)
	}
	return result
}

func (adapter *adapter) validateCandidateMountPaths(candidate Candidate) error {
	for index, mount := range adapter.definition.mounts {
		if mount.source != adapter.expectedMountSource(index, candidate.ConfigurationDigest, candidate.SecretRevisionDigest) {
			if index == 0 {
				return ErrConfiguration
			}
			if mount.revision {
				return ErrSecret
			}
			return ErrConflict
		}
	}
	return nil
}

func (adapter *adapter) expectedMountSource(index int, configurationDigest, secretDigest string) string {
	mount := adapter.definition.mounts[index]
	if index == 0 {
		return filepath.Join(filepath.Dir(adapter.configPath), strings.TrimPrefix(configurationDigest, "sha256:"))
	}
	if mount.revision {
		store := filepath.Dir(filepath.Dir(mount.source))
		return filepath.Join(store, strings.TrimPrefix(secretDigest, "sha256:"), filepath.Base(mount.source))
	}
	return mount.source
}

func (adapter *adapter) bindingDigest() string {
	value := adapter.dockerBinary + "\x00" + adapter.dockerDigest + "\x00" + adapter.statePath + "\x00" + adapter.configPath + "\x00" + adapter.prefix + "\x00" + adapter.network + "\x00" + adapter.definition.adapterID + "\x00" + dockerHost
	for _, mount := range adapter.definition.mounts {
		value += "\x00" + mount.source + "\x00" + mount.destination
	}
	return digest([]byte(value))
}

func (adapter *adapter) containerName(componentID string) string {
	return adapter.prefix + "-" + componentID + "-" + string(adapter.definition.role)
}

func (adapter *adapter) rollbackName(containerName, operationID string) string {
	operationDigest := digest([]byte(operationID))
	return containerName + "-rollback-" + operationDigest[len("sha256:"):len("sha256:")+12]
}

func summary(metadata transactionMetadata) Summary {
	result := Summary{
		SchemaVersion: RuntimeSchemaVersion, Role: metadata.Role, OperationID: metadata.OperationID,
		ComponentID: metadata.ComponentID, BindingDigest: metadata.BindingDigest, RollbackReference: metadata.RollbackReference,
		NetworkDigest:         metadata.NetworkDigest,
		CandidateConfigDigest: metadata.Candidate.Candidate.ConfigurationDigest,
		CandidateImageDigest:  imageDigest(metadata.Candidate.Candidate.Image), HadPrevious: metadata.Previous != nil,
		PreviousRunning: metadata.PreviousRunning,
	}
	if metadata.Previous != nil {
		result.PreviousConfigDigest = metadata.Previous.Candidate.ConfigurationDigest
		result.PreviousImageDigest = imageDigest(metadata.Previous.Candidate.Image)
	}
	return result
}
