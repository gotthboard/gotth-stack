package mailruntime

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

const maxTransactionBytes = 2 << 20

func (prepared *Prepared) Stage() (Summary, error) {
	if prepared == nil || prepared.adapter == nil {
		return Summary{}, ErrInvalidInput
	}
	return prepared.adapter.stage(prepared)
}

func (adapter *adapter) stage(prepared *Prepared) (Summary, error) {
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
			if err := syncRoot(adapter.transactions); err != nil {
				return Summary{}, err
			}
			return summary(existing), nil
		}
		return Summary{}, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, err
	}
	if _, err := adapter.transactions.Lstat(stageName(metadata.OperationID)); err == nil {
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
	if err != nil || len(encoded)+1 > maxTransactionBytes {
		return Summary{}, ErrStorage
	}
	encoded = append(encoded, '\n')
	temporary := stageName(metadata.OperationID)
	file, err := adapter.transactions.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Summary{}, ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
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
	if err := adapter.transactions.Rename(temporary, transactionName(metadata.OperationID)); err != nil {
		return Summary{}, ErrStorage
	}
	if err := syncRoot(adapter.transactions); err != nil {
		return Summary{}, err
	}
	return summary(metadata), nil
}

func (adapter *adapter) reconcileStage(operationID string) (Summary, State, error) {
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
	if err := adapter.transactions.Rename(stageName(operationID), transactionName(operationID)); err != nil {
		return Summary{}, StateIncomplete, ErrStorage
	}
	if err := syncRoot(adapter.transactions); err != nil {
		return Summary{}, StateCandidate, err
	}
	return summary(metadata), StateCandidate, nil
}

func (adapter *adapter) rollbackStage(ctx context.Context, operationID string) error {
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
		if err := adapter.transactions.Remove(transactionName(operationID)); err != nil {
			return ErrStorage
		}
		return syncRoot(adapter.transactions)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name := stageName(operationID)
	if _, err := adapter.transactions.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrStorage
	}
	if err := adapter.transactions.Remove(name); err != nil {
		return ErrStorage
	}
	return syncRoot(adapter.transactions)
}

func (adapter *adapter) verifyPreflightState(ctx context.Context, metadata transactionMetadata) error {
	if err := adapter.validateBindings(ctx, metadata); err != nil {
		return err
	}
	rollback, err := adapter.inspectContainer(ctx, adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID), metadata.Previous)
	if err != nil || rollback != nil {
		return ErrConflict
	}
	primary, err := adapter.inspectContainer(ctx, metadata.Candidate.ContainerName, metadata.Previous)
	if err != nil {
		return err
	}
	if metadata.Previous == nil {
		if primary != nil {
			return ErrConflict
		}
		return nil
	}
	if primary == nil || !recordMatches(primary, *metadata.Previous) || primary.running != metadata.PreviousRunning {
		return ErrConflict
	}
	return nil
}

func (adapter *adapter) validateBindings(ctx context.Context, metadata transactionMetadata) error {
	network, exists, err := adapter.inspectNetwork(ctx)
	if err != nil || !exists || network.Digest != metadata.NetworkDigest {
		return ErrConflict
	}
	engine, err := adapter.engineDigest(ctx)
	if err != nil || engine != metadata.EngineDigest {
		return ErrConflict
	}
	if err := adapter.verifyMountIdentities(metadata.Mounts); err != nil {
		return err
	}
	if err := adapter.verifyConfiguration(metadata.Candidate.Candidate.ConfigurationMembers, metadata.Candidate.Candidate.ConfigurationDigest, metadata.Candidate.Candidate.ConfigurationSize); err != nil {
		return err
	}
	revision, err := adapter.revisionDigest()
	if err != nil || revision != metadata.Candidate.Candidate.SecretRevisionDigest {
		return ErrSecret
	}
	imageID, err := adapter.inspectImage(ctx, metadata.Candidate)
	if err != nil || imageID != metadata.Candidate.ImageID {
		return ErrImage
	}
	if metadata.Previous != nil {
		previousID, err := adapter.inspectImage(ctx, *metadata.Previous)
		if err != nil || previousID != metadata.Previous.ImageID {
			return ErrImage
		}
	}
	return nil
}

func (adapter *adapter) loadTransaction(operationID string) (transactionMetadata, error) {
	if !validID(operationID) {
		return transactionMetadata{}, ErrInvalidInput
	}
	metadata, err := adapter.loadTransactionFile(transactionName(operationID), operationID)
	if err != nil {
		return transactionMetadata{}, err
	}
	if _, err := adapter.transactions.Lstat(stageName(operationID)); err == nil {
		return transactionMetadata{}, ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		return transactionMetadata{}, ErrStorage
	}
	return metadata, nil
}

func (adapter *adapter) loadTransactionFile(name, operationID string) (transactionMetadata, error) {
	file, err := adapter.transactions.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return transactionMetadata{}, os.ErrNotExist
		}
		return transactionMetadata{}, ErrStorage
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 || info.Size() < 1 || info.Size() > maxTransactionBytes {
		return transactionMetadata{}, ErrUnsafeState
	}
	value, err := io.ReadAll(io.LimitReader(file, maxTransactionBytes+1))
	if err != nil || len(value) > maxTransactionBytes {
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

func (adapter *adapter) validMetadata(metadata transactionMetadata) bool {
	if metadata.SchemaVersion != RuntimeSchemaVersion || metadata.Role != adapter.definition.role || !validID(metadata.OperationID) || !validID(metadata.ComponentID) || metadata.BindingDigest != adapter.bindingDigest() || !digestPattern.MatchString(metadata.NetworkDigest) || !digestPattern.MatchString(metadata.EngineDigest) || !adapter.validStoredSpec(metadata.Candidate) || metadata.Candidate.Candidate.OperationID != metadata.OperationID || metadata.Candidate.Candidate.ComponentID != metadata.ComponentID || metadata.Candidate.ContainerName != adapter.containerName(metadata.ComponentID) || metadata.Candidate.NetworkName != adapter.network || len(metadata.Mounts) != len(adapter.definition.mounts) || !digestPattern.MatchString(metadata.RollbackReference) {
		return false
	}
	for index, mount := range metadata.Mounts {
		if mount.Destination != adapter.definition.mounts[index].destination || mount.Identity.Device == 0 || mount.Identity.Inode == 0 {
			return false
		}
	}
	if metadata.Previous != nil {
		if !adapter.validStoredSpec(*metadata.Previous) || metadata.Previous.Candidate.ComponentID != metadata.ComponentID || metadata.Previous.ContainerName != metadata.Candidate.ContainerName || metadata.Previous.NetworkName != adapter.network || metadata.Previous.Role != metadata.Role {
			return false
		}
	}
	unsigned := metadata
	unsigned.RollbackReference = ""
	encoded, err := json.Marshal(unsigned)
	return err == nil && digest(encoded) == metadata.RollbackReference
}

func (adapter *adapter) validStoredSpec(spec managedSpec) bool {
	if !validCandidate(spec.Candidate, adapter.definition) || spec.Role != adapter.definition.role || !hexIDPattern.MatchString(spec.ImageID) || spec.ContainerName == "" || spec.NetworkName != adapter.network || len(spec.MountSources) != len(adapter.definition.mounts) {
		return false
	}
	for index, source := range spec.MountSources {
		wanted := adapter.expectedMountSource(index, spec.Candidate.ConfigurationDigest, spec.Candidate.SecretRevisionDigest)
		if source != wanted {
			return false
		}
	}
	return true
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

func transactionName(operationID string) string { return operationID + ".json" }
func stageName(operationID string) string       { return "." + operationID + ".stage" }
