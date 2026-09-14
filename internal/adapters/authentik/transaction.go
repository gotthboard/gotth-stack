package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"syscall"
)

func (adapter *Adapter) Stage(prepared *Prepared) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	if prepared == nil || prepared.adapter != adapter || !adapter.validMetadata(prepared.metadata) {
		return Summary{}, ErrInvalidInput
	}
	metadata := prepared.metadata
	if existing, err := adapter.loadTransaction(metadata.OperationID); err == nil {
		if reflect.DeepEqual(existing, metadata) {
			if err := syncRoot(adapter.transactionsRoot); err != nil {
				return Summary{}, err
			}
			return summary(existing), nil
		}
		return Summary{}, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, err
	}
	if _, err := adapter.transactionsRoot.Lstat(stageName(metadata.OperationID)); err == nil {
		return Summary{}, ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, ErrStorage
	}
	ctx, cancel := context.WithTimeout(context.Background(), adapter.timeout)
	defer cancel()
	if err := adapter.verifyPreflightState(ctx, metadata); err != nil {
		return Summary{}, err
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return Summary{}, ErrStorage
	}
	encoded = append(encoded, '\n')
	temporary := stageName(metadata.OperationID)
	file, err := adapter.transactionsRoot.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Summary{}, ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			file.Close()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return Summary{}, ErrStorage
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 {
		return Summary{}, ErrUnsafeState
	}
	if _, err := file.Write(encoded); err != nil || file.Sync() != nil {
		return Summary{}, ErrStorage
	}
	closed = true
	if err := file.Close(); err != nil {
		return Summary{}, ErrStorage
	}
	if err := adapter.transactionsRoot.Rename(temporary, transactionName(metadata.OperationID)); err != nil {
		return Summary{}, ErrStorage
	}
	if err := syncRoot(adapter.transactionsRoot); err != nil {
		return Summary{}, err
	}
	return summary(metadata), nil
}

func (adapter *Adapter) ReconcileStage(operationID string) (Summary, State, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, StateAbsent, ErrClosed
	}
	if !validID(operationID) {
		return Summary{}, StateAbsent, ErrInvalidInput
	}
	if metadata, err := adapter.loadTransaction(operationID); err == nil {
		return summary(metadata), StateCandidate, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, StateOther, err
	}
	metadata, err := adapter.loadTransactionFile(stageName(operationID), operationID)
	if errors.Is(err, os.ErrNotExist) {
		return Summary{}, StateAbsent, nil
	}
	if err != nil {
		return Summary{}, StateIncomplete, ErrRecoveryRequired
	}
	if err := adapter.transactionsRoot.Rename(stageName(operationID), transactionName(operationID)); err != nil {
		return Summary{}, StateIncomplete, ErrStorage
	}
	if err := syncRoot(adapter.transactionsRoot); err != nil {
		return Summary{}, StateCandidate, err
	}
	return summary(metadata), StateCandidate, nil
}

func (adapter *Adapter) RollbackStage(ctx context.Context, operationID string) error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return ErrClosed
	}
	if !validID(operationID) {
		return ErrInvalidInput
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	metadata, err := adapter.loadTransaction(operationID)
	if err == nil {
		if err := adapter.verifyPreflightState(bounded, metadata); err != nil {
			return ErrRecoveryRequired
		}
		if err := adapter.transactionsRoot.Remove(transactionName(operationID)); err != nil {
			return ErrStorage
		}
		return syncRoot(adapter.transactionsRoot)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name := stageName(operationID)
	if _, err := adapter.transactionsRoot.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrStorage
	}
	if err := adapter.transactionsRoot.Remove(name); err != nil {
		return ErrStorage
	}
	return syncRoot(adapter.transactionsRoot)
}

func (adapter *Adapter) verifyPreflightState(ctx context.Context, metadata transactionMetadata) error {
	engine, err := adapter.engineDigest(ctx)
	if err != nil || engine != metadata.EngineDigest {
		return ErrConflict
	}
	if err := adapter.validateFileBindings(metadata); err != nil {
		return err
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		rollback, inspectErr := adapter.inspectContainer(ctx, adapter.rollbackName(adapter.containerName(metadata.ComponentID, role), metadata.OperationID))
		if inspectErr != nil || rollback != nil {
			return ErrConflict
		}
	}
	server, err := adapter.inspectContainer(ctx, adapter.containerName(metadata.ComponentID, RoleServer))
	if err != nil {
		return err
	}
	worker, err := adapter.inspectContainer(ctx, adapter.containerName(metadata.ComponentID, RoleWorker))
	if err != nil {
		return err
	}
	if metadata.Previous == nil {
		if server != nil || worker != nil {
			return ErrConflict
		}
		return nil
	}
	if !recordMatches(server, *metadata.Previous, RoleServer) || !recordMatches(worker, *metadata.Previous, RoleWorker) || server.running != metadata.PreviousServerRunning || worker.running != metadata.PreviousWorkerRunning {
		return ErrConflict
	}
	return nil
}

func (adapter *Adapter) loadTransaction(operationID string) (transactionMetadata, error) {
	if !validID(operationID) {
		return transactionMetadata{}, ErrInvalidInput
	}
	metadata, err := adapter.loadTransactionFile(transactionName(operationID), operationID)
	if err != nil {
		return transactionMetadata{}, err
	}
	if _, err := adapter.transactionsRoot.Lstat(stageName(operationID)); err == nil {
		return transactionMetadata{}, ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		return transactionMetadata{}, ErrStorage
	}
	return metadata, nil
}
func (adapter *Adapter) loadTransactionFile(name, operationID string) (transactionMetadata, error) {
	file, err := adapter.transactionsRoot.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return transactionMetadata{}, os.ErrNotExist
		}
		return transactionMetadata{}, ErrStorage
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 || info.Size() < 1 || info.Size() > 64<<10 {
		return transactionMetadata{}, ErrUnsafeState
	}
	value, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(value) > 64<<10 {
		return transactionMetadata{}, ErrStorage
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var metadata transactionMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return transactionMetadata{}, ErrUnsafeState
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || metadata.OperationID != operationID || !adapter.validMetadata(metadata) {
		return transactionMetadata{}, ErrUnsafeState
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(value, append(canonical, '\n')) {
		return transactionMetadata{}, ErrUnsafeState
	}
	return metadata, nil
}

func (adapter *Adapter) validMetadata(metadata transactionMetadata) bool {
	if metadata.SchemaVersion != SchemaVersion || !validID(metadata.OperationID) || !validID(metadata.ComponentID) || metadata.BindingDigest != adapter.bindingDigest() || !digestPattern.MatchString(metadata.EngineDigest) || !validStoredSpec(metadata.Candidate) || metadata.Candidate.OperationID != metadata.OperationID || metadata.Candidate.ComponentID != metadata.ComponentID || !validIdentity(metadata.DataIdentity, 0o700) || !validIdentity(metadata.CertsIdentity, 0o700) || !validIdentity(metadata.TemplatesIdentity, 0o700) || !validSecretIdentity(metadata.DatabaseSecretIdentity) || !validSecretIdentity(metadata.KeySecretIdentity) || !digestPattern.MatchString(metadata.RollbackReference) {
		return false
	}
	if metadata.Previous != nil && (!validStoredSpec(*metadata.Previous) || !sameDurableIdentity(*metadata.Previous, metadata.Candidate)) {
		return false
	}
	unsigned := metadata
	unsigned.RollbackReference = ""
	encoded, err := json.Marshal(unsigned)
	return err == nil && digest(encoded) == metadata.RollbackReference
}
func validStoredSpec(spec managedSpec) bool {
	computed, err := configurationDigest(spec.Specification)
	return err == nil && computed == spec.ConfigurationDigest && validID(spec.OperationID) && validID(spec.ComponentID) && digestPattern.MatchString(spec.DatabaseSecretRevision) && digestPattern.MatchString(spec.KeySecretRevision) && digestPattern.MatchString(spec.ImageID)
}
func validIdentity(value fileIdentityJSON, mode uint32) bool {
	return value.Device != 0 && value.Inode != 0 && value.Mode == mode
}
func validSecretIdentity(value fileIdentityJSON) bool {
	return value.Device != 0 && value.Inode != 0 && value.Size >= 1 && value.Size <= MaxSecretBytes && value.Mode&0o007 == 0 && value.Mode&0o222 == 0
}
func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return ErrStorage
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return ErrStorage
	}
	return nil
}
func transactionName(operation string) string { return operation + ".json" }
func stageName(operation string) string       { return "." + operation + ".stage" }

func (adapter *Adapter) validateFileBindings(metadata transactionMetadata) error {
	data, certs, templates, err := adapter.readDirectoryIdentities(metadata.Candidate)
	if err != nil || !directoryIdentityMatches(metadata.DataIdentity, data) || !directoryIdentityMatches(metadata.CertsIdentity, certs) || !directoryIdentityMatches(metadata.TemplatesIdentity, templates) {
		return ErrConflict
	}
	database, err := adapter.readSecretIdentity(metadata.Candidate, metadata.ComponentID+"-postgresql-password", metadata.Candidate.DatabaseSecretRevision)
	if err != nil || !identityMatches(metadata.DatabaseSecretIdentity, database) {
		return ErrConflict
	}
	key, err := adapter.readSecretIdentity(metadata.Candidate, metadata.ComponentID+"-secret-key", metadata.Candidate.KeySecretRevision)
	if err != nil || !identityMatches(metadata.KeySecretIdentity, key) {
		return ErrConflict
	}
	return nil
}
