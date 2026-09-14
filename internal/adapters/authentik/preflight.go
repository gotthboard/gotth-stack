package authentik

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
	if !validID(request.OperationID) || !validID(request.ComponentID) || !digestPattern.MatchString(request.ConfigurationDigest) || !digestPattern.MatchString(request.DatabaseSecretRevisionDigest) || !digestPattern.MatchString(request.KeySecretRevisionDigest) {
		return nil, Summary{}, ErrInvalidInput
	}
	computed, err := configurationDigest(request.Specification)
	if err != nil || computed != request.ConfigurationDigest {
		return nil, Summary{}, ErrConflict
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	defer cancel()
	candidate := managedSpec{OperationID: request.OperationID, ComponentID: request.ComponentID, ConfigurationDigest: request.ConfigurationDigest, DatabaseSecretRevision: request.DatabaseSecretRevisionDigest, KeySecretRevision: request.KeySecretRevisionDigest, Specification: request.Specification}
	candidate.ImageID, err = adapter.inspectImage(bounded, candidate)
	if err != nil {
		return nil, Summary{}, err
	}
	engine, err := adapter.engineDigest(bounded)
	if err != nil {
		return nil, Summary{}, err
	}
	dataID, certsID, templatesID, err := adapter.readDirectoryIdentities(candidate)
	if err != nil {
		return nil, Summary{}, err
	}
	databaseSecretID, err := adapter.readSecretIdentity(candidate, candidate.ComponentID+"-postgresql-password", candidate.DatabaseSecretRevision)
	if err != nil {
		return nil, Summary{}, err
	}
	keySecretID, err := adapter.readSecretIdentity(candidate, candidate.ComponentID+"-secret-key", candidate.KeySecretRevision)
	if err != nil {
		return nil, Summary{}, err
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		rollback, inspectErr := adapter.inspectContainer(bounded, adapter.rollbackName(adapter.containerName(candidate.ComponentID, role), candidate.OperationID))
		if inspectErr != nil {
			return nil, Summary{}, inspectErr
		}
		if rollback != nil {
			return nil, Summary{}, ErrRecoveryRequired
		}
	}
	server, err := adapter.inspectContainer(bounded, adapter.containerName(candidate.ComponentID, RoleServer))
	if err != nil {
		return nil, Summary{}, err
	}
	worker, err := adapter.inspectContainer(bounded, adapter.containerName(candidate.ComponentID, RoleWorker))
	if err != nil {
		return nil, Summary{}, err
	}
	var previous *managedSpec
	serverRunning, workerRunning := false, false
	if server != nil || worker != nil {
		if server == nil || worker == nil {
			return nil, Summary{}, ErrRecoveryRequired
		}
		value, pairErr := managedFromPair(server, worker)
		if pairErr != nil {
			return nil, Summary{}, pairErr
		}
		if !sameDurableIdentity(value, candidate) {
			return nil, Summary{}, ErrConflict
		}
		previousID, imageErr := adapter.inspectImage(bounded, value)
		if imageErr != nil || previousID != value.ImageID {
			return nil, Summary{}, ErrImage
		}
		copy := value
		previous = &copy
		serverRunning = server.running
		workerRunning = worker.running
		if workerRunning && !serverRunning {
			return nil, Summary{}, ErrRecoveryRequired
		}
		if value.ConfigurationDigest == candidate.ConfigurationDigest {
			return nil, Summary{}, ErrConflict
		}
	}
	metadata := transactionMetadata{SchemaVersion: SchemaVersion, OperationID: request.OperationID, ComponentID: request.ComponentID, BindingDigest: adapter.bindingDigest(), EngineDigest: engine, Candidate: candidate, Previous: previous, PreviousServerRunning: serverRunning, PreviousWorkerRunning: workerRunning, DataIdentity: identityJSON(dataID), CertsIdentity: identityJSON(certsID), TemplatesIdentity: identityJSON(templatesID), DatabaseSecretIdentity: identityJSON(databaseSecretID), KeySecretIdentity: identityJSON(keySecretID)}
	unsigned, err := json.Marshal(metadata)
	if err != nil {
		return nil, Summary{}, ErrStorage
	}
	metadata.RollbackReference = digest(unsigned)
	return &Prepared{adapter: adapter, metadata: metadata}, summary(metadata), nil
}

func (adapter *Adapter) bindingDigest() string {
	return digest([]byte(adapter.dockerBinary + "\x00" + adapter.dockerDigest + "\x00" + adapter.statePath + "\x00" + adapter.dataPath + "\x00" + adapter.secretPath + "\x00" + adapter.prefix + "\x00" + dockerHost))
}
func (adapter *Adapter) containerName(component string, role Role) string {
	return adapter.prefix + "-" + component + "-" + string(role)
}
func (adapter *Adapter) rollbackName(name, operation string) string {
	value := digest([]byte(operation))
	return name + "-rollback-" + value[len("sha256:"):len("sha256:")+12]
}
func (adapter *Adapter) paths(spec managedSpec) (string, string, string, string, string) {
	base := filepath.Join(adapter.dataPath, spec.ComponentID)
	return filepath.Join(base, "data"), filepath.Join(base, "certs"), filepath.Join(base, "templates"), filepath.Join(adapter.secretPath, spec.ComponentID+"-postgresql-password"), filepath.Join(adapter.secretPath, spec.ComponentID+"-secret-key")
}
func summary(metadata transactionMetadata) Summary {
	result := Summary{SchemaVersion: SchemaVersion, OperationID: metadata.OperationID, ComponentID: metadata.ComponentID, BindingDigest: metadata.BindingDigest, RollbackReference: metadata.RollbackReference, CandidateConfigDigest: metadata.Candidate.ConfigurationDigest, CandidateImageDigest: imageDigest(metadata.Candidate.Specification.Image), HadPrevious: metadata.Previous != nil, PreviousServerRunning: metadata.PreviousServerRunning, PreviousWorkerRunning: metadata.PreviousWorkerRunning}
	if metadata.Previous != nil {
		result.PreviousConfigDigest = metadata.Previous.ConfigurationDigest
		result.PreviousImageDigest = imageDigest(metadata.Previous.Specification.Image)
	}
	return result
}
