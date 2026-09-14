package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"
	"syscall"
)

const (
	metadataName  = "metadata.json"
	previousName  = "previous.caddyfile"
	candidateName = "candidate.caddyfile"
)

type transaction struct {
	metadata  transactionMetadata
	previous  []byte
	candidate []byte
}

func (adapter *Adapter) Stage(prepared *Prepared) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	if prepared == nil || prepared.adapter != adapter || !validMetadata(prepared.metadata) {
		return Summary{}, ErrInvalidInput
	}
	operationID := prepared.metadata.OperationID
	if existing, err := adapter.loadTransaction(operationID); err == nil {
		if reflect.DeepEqual(existing.metadata, prepared.metadata) && bytes.Equal(existing.previous, prepared.previous) && bytes.Equal(existing.candidate, prepared.candidate) {
			if err := adapter.ops.syncRoot(adapter.transactionsRoot); err != nil {
				return Summary{}, ErrStorage
			}
			return summary(existing.metadata), nil
		}
		return Summary{}, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, err
	}
	current, identity, err := adapter.readTarget()
	if err != nil {
		return Summary{}, err
	}
	if identity.device != prepared.target.device || identity.inode != prepared.target.inode || identity.mode != prepared.target.mode ||
		identity.uid != prepared.target.uid || identity.gid != prepared.target.gid || !bytes.Equal(current, prepared.previous) {
		return Summary{}, ErrConflict
	}
	temporary := temporaryStageName(operationID)
	if _, err := adapter.transactionsRoot.Lstat(temporary); err == nil {
		return Summary{}, ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, ErrStorage
	}
	if err := adapter.transactionsRoot.Mkdir(temporary, 0o700); err != nil {
		return Summary{}, ErrStorage
	}
	root, err := adapter.transactionsRoot.OpenRoot(temporary)
	if err != nil {
		return Summary{}, ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = root.Close()
		}
	}()
	if err := adapter.createSyncedFile(root, previousName, prepared.previous); err != nil {
		return Summary{}, err
	}
	if err := adapter.createSyncedFile(root, candidateName, prepared.candidate); err != nil {
		return Summary{}, err
	}
	encoded, err := json.Marshal(prepared.metadata)
	if err != nil {
		return Summary{}, ErrStorage
	}
	encoded = append(encoded, '\n')
	if err := adapter.createSyncedFile(root, metadataName, encoded); err != nil {
		return Summary{}, err
	}
	if err := adapter.ops.syncRoot(root); err != nil {
		return Summary{}, ErrStorage
	}
	closed = true
	if err := root.Close(); err != nil {
		return Summary{}, ErrStorage
	}
	if err := adapter.ops.rename(adapter.transactionsRoot, temporary, operationID); err != nil {
		return Summary{}, ErrStorage
	}
	if err := adapter.ops.syncRoot(adapter.transactionsRoot); err != nil {
		return Summary{}, ErrStorage
	}
	return summary(prepared.metadata), nil
}

// ReconcileStage resolves an interrupted staging mutation from observable
// private state. A complete temporary transaction is promoted atomically; a
// partial one remains recovery-required and is never guessed or discarded.
func (adapter *Adapter) ReconcileStage(operationID string) (Summary, State, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, StateAbsent, ErrClosed
	}
	if !validID(operationID) {
		return Summary{}, StateAbsent, ErrInvalidInput
	}
	if transaction, err := adapter.loadTransaction(operationID); err == nil {
		if err := adapter.ops.syncRoot(adapter.transactionsRoot); err != nil {
			return Summary{}, StatePresent, ErrStorage
		}
		return summary(transaction.metadata), StatePresent, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Summary{}, StateAbsent, err
	}
	temporary := temporaryStageName(operationID)
	if _, err := adapter.transactionsRoot.Lstat(temporary); errors.Is(err, os.ErrNotExist) {
		return Summary{}, StateAbsent, nil
	} else if err != nil {
		return Summary{}, StateIncomplete, ErrStorage
	}
	transaction, err := adapter.loadTransactionAt(temporary, operationID)
	if err != nil {
		return Summary{}, StateIncomplete, ErrRecoveryRequired
	}
	if err := adapter.ops.rename(adapter.transactionsRoot, temporary, operationID); err != nil {
		return Summary{}, StateIncomplete, ErrStorage
	}
	if err := adapter.ops.syncRoot(adapter.transactionsRoot); err != nil {
		return Summary{}, StatePresent, ErrStorage
	}
	return summary(transaction.metadata), StatePresent, nil
}

// RollbackStage removes only private staging state. A committed transaction
// may be removed only while both live state domains still equal its recorded
// preflight baseline, so this cannot erase the rollback material for an
// installed candidate.
func (adapter *Adapter) RollbackStage(ctx context.Context, operationID string) error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return ErrClosed
	}
	if !validID(operationID) {
		return ErrInvalidInput
	}
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	transaction, err := adapter.loadTransaction(operationID)
	if err == nil {
		file, _, readErr := adapter.readTargetRaw()
		if readErr != nil {
			return readErr
		}
		runtimeValue, readErr := adapter.runtime.Read(ctx)
		if readErr != nil {
			return readErr
		}
		if digest(file) != transaction.metadata.PreviousFileDigest || digest(runtimeValue) != transaction.metadata.PreviousRuntimeDigest {
			return ErrRecoveryRequired
		}
		temporaryState, stateErr := adapter.configTemporaryState(operationID, transaction.metadata)
		if stateErr != nil {
			return stateErr
		}
		if temporaryState != StateAbsent {
			return ErrRecoveryRequired
		}
		return adapter.removeTransaction(operationID)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary := temporaryStageName(operationID)
	if _, err := adapter.transactionsRoot.Lstat(temporary); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrStorage
	}
	return adapter.removeTransaction(temporary)
}

func (adapter *Adapter) removeTransaction(name string) error {
	info, err := adapter.transactionsRoot.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return ErrUnsafeState
	}
	if err := adapter.transactionsRoot.RemoveAll(name); err != nil {
		return ErrStorage
	}
	if err := adapter.ops.syncRoot(adapter.transactionsRoot); err != nil {
		return ErrStorage
	}
	return nil
}

func (adapter *Adapter) createSyncedFile(root *os.Root, name string, value []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := adapter.ops.write(file, value); err != nil {
		return ErrStorage
	}
	if err := adapter.ops.sync(file); err != nil {
		return ErrStorage
	}
	closed = true
	if err := adapter.ops.close(file); err != nil {
		return ErrStorage
	}
	return nil
}

func (adapter *Adapter) loadTransaction(operationID string) (transaction, error) {
	if !validID(operationID) {
		return transaction{}, ErrInvalidInput
	}
	return adapter.loadTransactionAt(operationID, operationID)
}

func (adapter *Adapter) loadTransactionAt(name, operationID string) (transaction, error) {
	info, err := adapter.transactionsRoot.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return transaction{}, os.ErrNotExist
		}
		return transaction{}, ErrStorage
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return transaction{}, ErrUnsafeState
	}
	root, err := adapter.transactionsRoot.OpenRoot(name)
	if err != nil {
		return transaction{}, ErrUnsafeState
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil || len(entries) != 3 {
		return transaction{}, ErrUnsafeState
	}
	seen := make(map[string]bool, 3)
	for _, entry := range entries {
		name := entry.Name()
		if name != metadataName && name != previousName && name != candidateName {
			return transaction{}, ErrUnsafeState
		}
		seen[name] = true
	}
	if !seen[metadataName] || !seen[previousName] || !seen[candidateName] {
		return transaction{}, ErrUnsafeState
	}
	metadataBytes, err := readPrivateFile(root, metadataName, 4096)
	if err != nil {
		return transaction{}, err
	}
	var metadata transactionMetadata
	if err := decodeCanonical(metadataBytes, &metadata); err != nil || !validMetadata(metadata) || metadata.OperationID != operationID || metadata.BindingDigest != adapter.bindingDigest() {
		return transaction{}, ErrUnsafeState
	}
	previous, err := readPrivateFile(root, previousName, MaxConfigBytes)
	if err != nil {
		return transaction{}, err
	}
	candidate, err := readPrivateFile(root, candidateName, MaxConfigBytes)
	if err != nil {
		return transaction{}, err
	}
	if validateConfiguration(previous) != nil || validateConfiguration(candidate) != nil || digest(previous) != metadata.PreviousFileDigest || digest(candidate) != metadata.CandidateFileDigest {
		return transaction{}, ErrUnsafeState
	}
	return transaction{metadata: metadata, previous: previous, candidate: candidate}, nil
}

func validMetadata(metadata transactionMetadata) bool {
	if metadata.SchemaVersion != SchemaVersion || !validID(metadata.OperationID) || !validID(metadata.ComponentID) || !digestPattern.MatchString(metadata.BindingDigest) ||
		!digestPattern.MatchString(metadata.PreviousFileDigest) || !digestPattern.MatchString(metadata.CandidateFileDigest) ||
		!digestPattern.MatchString(metadata.PreviousRuntimeDigest) || !digestPattern.MatchString(metadata.CandidateRuntimeDigest) ||
		!digestPattern.MatchString(metadata.RollbackReference) || metadata.ConfigMode > 0o777 || !safeConfigMode(os.FileMode(metadata.ConfigMode)) {
		return false
	}
	unsigned := metadata
	unsigned.RollbackReference = ""
	encoded, err := json.Marshal(unsigned)
	return err == nil && digest(encoded) == metadata.RollbackReference
}

func decodeCanonical(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrUnsafeState
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrUnsafeState
	}
	encoded, err := json.Marshal(target)
	if err != nil || !bytes.Equal(value, append(encoded, '\n')) {
		return ErrUnsafeState
	}
	return nil
}

func readPrivateFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafeState
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 || info.Size() < 1 {
		return nil, ErrUnsafeState
	}
	if info.Size() > limit {
		return nil, ErrLimit
	}
	value, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, ErrStorage
	}
	if int64(len(value)) > limit {
		return nil, ErrLimit
	}
	return value, nil
}

func temporaryStageName(operationID string) string {
	return "." + operationID + ".stage"
}
