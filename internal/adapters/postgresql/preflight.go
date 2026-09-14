package postgresql

import (
	"context"
	"encoding/json"
	"path/filepath"
)

func (adapter *Adapter) Preflight(ctx context.Context, request Request) (*Prepared, Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil, Summary{}, ErrClosed
	}
	if !validID(request.OperationID) || !validID(request.ComponentID) || !digestPattern.MatchString(request.ConfigurationDigest) || !digestPattern.MatchString(request.SecretRevisionDigest) {
		return nil, Summary{}, ErrInvalidInput
	}
	computed, err := configurationDigest(request.Specification)
	if err != nil || computed != request.ConfigurationDigest {
		return nil, Summary{}, ErrConflict
	}
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	defer cancel()
	candidate := managedSpec{
		OperationID: request.OperationID, ComponentID: request.ComponentID,
		ConfigurationDigest: request.ConfigurationDigest, SecretRevision: request.SecretRevisionDigest,
		Specification: request.Specification, ContainerName: adapter.containerName(request.ComponentID),
		DataName: request.ComponentID, SecretName: request.ComponentID + "-password",
	}
	candidate.ImageID, err = adapter.inspectImage(ctx, candidate)
	if err != nil {
		return nil, Summary{}, err
	}
	engineDigest, err := adapter.engineDigest(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	dataIdentity, initialized, err := adapter.readDataIdentity(candidate)
	if err != nil {
		return nil, Summary{}, err
	}
	secretIdentity, err := adapter.readSecretIdentity(candidate)
	if err != nil {
		return nil, Summary{}, err
	}
	rollbackName := adapter.rollbackName(candidate.ContainerName, request.OperationID)
	rollback, err := adapter.inspectContainer(ctx, rollbackName)
	if err != nil {
		return nil, Summary{}, err
	}
	if rollback != nil {
		return nil, Summary{}, ErrRecoveryRequired
	}
	primary, err := adapter.inspectContainer(ctx, candidate.ContainerName)
	if err != nil {
		return nil, Summary{}, err
	}
	var previous *managedSpec
	previousRunning := false
	if primary != nil {
		if !sameDurableIdentity(primary.spec, candidate) {
			return nil, Summary{}, ErrConflict
		}
		previousImageID, imageErr := adapter.inspectImage(ctx, primary.spec)
		if imageErr != nil || previousImageID != primary.spec.ImageID {
			return nil, Summary{}, ErrImage
		}
		copy := primary.spec
		previous = &copy
		previousRunning = primary.running
	}
	metadata := transactionMetadata{
		SchemaVersion: SchemaVersion, OperationID: request.OperationID, ComponentID: request.ComponentID,
		BindingDigest: adapter.bindingDigest(), EngineDigest: engineDigest, Candidate: candidate,
		Previous: previous, PreviousRunning: previousRunning, DataInitialized: initialized,
		DataIdentity: identityJSON(dataIdentity), SecretIdentity: identityJSON(secretIdentity),
	}
	unsigned, err := json.Marshal(metadata)
	if err != nil {
		return nil, Summary{}, ErrStorage
	}
	metadata.RollbackReference = digest(unsigned)
	prepared := &Prepared{adapter: adapter, metadata: metadata}
	return prepared, summary(metadata), nil
}

func (adapter *Adapter) bindingDigest() string {
	value := adapter.dockerBinary + "\x00" + adapter.dockerDigest + "\x00" + adapter.statePath + "\x00" + adapter.dataPath + "\x00" + adapter.secretPath + "\x00" + adapter.prefix + "\x00" + dockerHost
	return digest([]byte(value))
}

func (adapter *Adapter) containerName(componentID string) string {
	return adapter.prefix + "-" + componentID
}

func (adapter *Adapter) rollbackName(containerName, operationID string) string {
	operationDigest := digest([]byte(operationID))
	return containerName + "-rollback-" + operationDigest[len("sha256:"):len("sha256:")+12]
}

func (adapter *Adapter) absoluteDataPath(spec managedSpec) string {
	return filepath.Join(adapter.dataPath, spec.DataName)
}

func (adapter *Adapter) absoluteSecretPath(spec managedSpec) string {
	return filepath.Join(adapter.secretPath, spec.SecretName)
}

func summary(metadata transactionMetadata) Summary {
	result := Summary{
		SchemaVersion: SchemaVersion, OperationID: metadata.OperationID, ComponentID: metadata.ComponentID,
		BindingDigest: metadata.BindingDigest, RollbackReference: metadata.RollbackReference,
		CandidateConfigDigest: metadata.Candidate.ConfigurationDigest,
		CandidateImageDigest:  imageDigest(metadata.Candidate.Specification.Image),
		HadPrevious:           metadata.Previous != nil, PreviousRunning: metadata.PreviousRunning,
	}
	if metadata.Previous != nil {
		result.PreviousConfigDigest = metadata.Previous.ConfigurationDigest
		result.PreviousImageDigest = imageDigest(metadata.Previous.Specification.Image)
	}
	return result
}
