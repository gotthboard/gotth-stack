package caddy

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func (adapter *Adapter) Observe(ctx context.Context, operationID string) (Observation, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Observation{}, err
	}
	defer cancel()
	return adapter.observe(ctx, operationID)
}

func (adapter *Adapter) observe(ctx context.Context, operationID string) (Observation, error) {
	if adapter.closed {
		return Observation{}, ErrClosed
	}
	if !validID(operationID) {
		return Observation{}, ErrInvalidInput
	}
	transaction, err := adapter.loadTransaction(operationID)
	if errors.Is(err, os.ErrNotExist) {
		state := StateAbsent
		if _, temporaryErr := adapter.transactionsRoot.Lstat(temporaryStageName(operationID)); temporaryErr == nil {
			state = StateIncomplete
		} else if !errors.Is(temporaryErr, os.ErrNotExist) {
			return Observation{}, ErrStorage
		}
		return Observation{StageState: state, FileState: StateAbsent, RuntimeState: StateAbsent}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	file, _, err := adapter.readTargetRaw()
	if err != nil {
		return Observation{}, err
	}
	fileDigest := digest(file)
	temporaryState, err := adapter.configTemporaryState(operationID, transaction.metadata)
	if err != nil {
		return Observation{}, err
	}
	runtimeValue, err := adapter.runtime.Read(ctx)
	if err != nil {
		return Observation{}, err
	}
	runtimeDigest := digest(runtimeValue)
	return Observation{
		Summary: summary(transaction.metadata), StageState: StatePresent,
		FileState:     classify(fileDigest, transaction.metadata.PreviousFileDigest, transaction.metadata.CandidateFileDigest),
		FileTemporary: temporaryState,
		RuntimeState:  classify(runtimeDigest, transaction.metadata.PreviousRuntimeDigest, transaction.metadata.CandidateRuntimeDigest),
		FileDigest:    fileDigest, RuntimeDigest: runtimeDigest,
	}, nil
}

func classify(observed, previous, candidate string) State {
	if observed == candidate {
		return StateCandidate
	}
	if observed == previous {
		return StatePrevious
	}
	return StateOther
}

func (adapter *Adapter) Install(operationID string) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	transaction, err := adapter.loadTransaction(operationID)
	if err != nil {
		return Summary{}, normalizeMissing(err)
	}
	current, _, err := adapter.readTargetRaw()
	if err != nil {
		return Summary{}, err
	}
	temporaryState, err := adapter.configTemporaryState(operationID, transaction.metadata)
	if err != nil {
		return Summary{}, err
	}
	if temporaryState != StateAbsent {
		return Summary{}, ErrRecoveryRequired
	}
	switch classify(digest(current), transaction.metadata.PreviousFileDigest, transaction.metadata.CandidateFileDigest) {
	case StateCandidate:
		return summary(transaction.metadata), nil
	case StatePrevious:
		if err := adapter.replaceTarget(operationID, transaction.candidate, transaction.metadata); err != nil {
			return Summary{}, err
		}
		return summary(transaction.metadata), nil
	default:
		return Summary{}, ErrRecoveryRequired
	}
}

func (adapter *Adapter) Restore(operationID string) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	transaction, err := adapter.loadTransaction(operationID)
	if err != nil {
		return Summary{}, normalizeMissing(err)
	}
	current, _, err := adapter.readTargetRaw()
	if err != nil {
		return Summary{}, err
	}
	if _, err := adapter.configTemporaryState(operationID, transaction.metadata); err != nil {
		return Summary{}, err
	}
	if err := adapter.removeConfigTemporary(operationID); err != nil {
		return Summary{}, err
	}
	switch classify(digest(current), transaction.metadata.PreviousFileDigest, transaction.metadata.CandidateFileDigest) {
	case StatePrevious:
		return summary(transaction.metadata), nil
	case StateCandidate:
		if err := adapter.replaceTarget(operationID, transaction.previous, transaction.metadata); err != nil {
			return Summary{}, err
		}
		return summary(transaction.metadata), nil
	default:
		return Summary{}, ErrRecoveryRequired
	}
}

func (adapter *Adapter) Activate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.activate(ctx, operationID, DesiredCandidate)
}

func (adapter *Adapter) Reactivate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.activate(ctx, operationID, DesiredPrevious)
}

func (adapter *Adapter) activate(ctx context.Context, operationID string, desired DesiredState) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	transaction, err := adapter.loadTransaction(operationID)
	if err != nil {
		return Summary{}, normalizeMissing(err)
	}
	file, _, err := adapter.readTargetRaw()
	if err != nil {
		return Summary{}, err
	}
	fileState := classify(digest(file), transaction.metadata.PreviousFileDigest, transaction.metadata.CandidateFileDigest)
	runtimeValue, err := adapter.runtime.Read(ctx)
	if err != nil {
		return Summary{}, err
	}
	runtimeState := classify(digest(runtimeValue), transaction.metadata.PreviousRuntimeDigest, transaction.metadata.CandidateRuntimeDigest)
	wantedState, allowedState, configName := StateCandidate, StatePrevious, candidateName
	expectedDigest := transaction.metadata.CandidateRuntimeDigest
	if desired == DesiredPrevious {
		wantedState, allowedState, configName = StatePrevious, StateCandidate, previousName
		expectedDigest = transaction.metadata.PreviousRuntimeDigest
	} else if desired != DesiredCandidate {
		return Summary{}, ErrInvalidInput
	}
	if fileState != wantedState {
		return Summary{}, ErrRecoveryRequired
	}
	if runtimeState == wantedState {
		return summary(transaction.metadata), nil
	}
	if runtimeState != allowedState {
		return Summary{}, ErrRecoveryRequired
	}
	configPath := filepath.Join(adapter.statePath, transactionDirName, operationID, configName)
	environment := []string{
		"PATH=/usr/bin:/bin", "HOME=" + adapter.statePath,
		"XDG_CONFIG_HOME=" + filepath.Join(adapter.statePath, scratchDirName),
		"XDG_DATA_HOME=" + filepath.Join(adapter.statePath, scratchDirName),
	}
	if _, err := adapter.run(ctx, commandRequest{kind: commandReload, configPath: configPath, adminAddress: adapter.adminAddress, workingDirectory: adapter.workingDirectory, environment: environment}, false); err != nil {
		return Summary{}, err
	}
	runtimeValue, err = adapter.runtime.Read(ctx)
	if err != nil {
		return Summary{}, err
	}
	if digest(runtimeValue) != expectedDigest {
		return Summary{}, ErrRecoveryRequired
	}
	return summary(transaction.metadata), nil
}

func (adapter *Adapter) Verify(ctx context.Context, operationID string, desired DesiredState) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	observation, err := adapter.observe(ctx, operationID)
	if err != nil {
		return Summary{}, err
	}
	wanted := StateCandidate
	if desired == DesiredPrevious {
		wanted = StatePrevious
	} else if desired != DesiredCandidate {
		return Summary{}, ErrInvalidInput
	}
	if observation.StageState != StatePresent || observation.FileState != wanted || observation.FileTemporary != StateAbsent || observation.RuntimeState != wanted {
		return Summary{}, ErrConflict
	}
	return observation.Summary, nil
}

func (adapter *Adapter) replaceTarget(operationID string, value []byte, metadata transactionMetadata) error {
	temporary := configTemporaryName(operationID)
	if _, err := adapter.configRoot.Lstat(temporary); err == nil {
		return ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrStorage
	}
	mode := os.FileMode(metadata.ConfigMode)
	file, err := adapter.configRoot.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := adapter.ops.chown(file, int(metadata.ConfigUID), int(metadata.ConfigGID)); err != nil {
		return ErrStorage
	}
	if err := adapter.ops.chmod(file, mode); err != nil {
		return ErrStorage
	}
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
	if err := adapter.ops.rename(adapter.configRoot, temporary, adapter.configName); err != nil {
		return ErrStorage
	}
	if err := adapter.ops.syncRoot(adapter.configRoot); err != nil {
		return ErrStorage
	}
	observed, identity, err := adapter.readTargetRaw()
	if err != nil || digest(observed) != digest(value) || identity.mode.Perm() != mode.Perm() || identity.uid != int(metadata.ConfigUID) || identity.gid != int(metadata.ConfigGID) {
		return ErrRecoveryRequired
	}
	return nil
}

func (adapter *Adapter) configTemporaryState(operationID string, metadata transactionMetadata) (State, error) {
	name := configTemporaryName(operationID)
	file, err := adapter.configRoot.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return StateAbsent, nil
	}
	if err != nil {
		return StateOther, ErrUnsafeState
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !safeConfigMode(info.Mode()) || !ownedByEffectiveUser(info) || linkCount(info) != 1 || info.Size() > MaxConfigBytes {
		return StateOther, ErrUnsafeState
	}
	value, err := io.ReadAll(io.LimitReader(file, MaxConfigBytes+1))
	if err != nil {
		return StateOther, ErrStorage
	}
	if len(value) > MaxConfigBytes {
		return StateOther, ErrLimit
	}
	return classify(digest(value), metadata.PreviousFileDigest, metadata.CandidateFileDigest), nil
}

func (adapter *Adapter) removeConfigTemporary(operationID string) error {
	name := configTemporaryName(operationID)
	if _, err := adapter.configRoot.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrStorage
	}
	if err := adapter.configRoot.Remove(name); err != nil {
		return ErrStorage
	}
	if err := adapter.ops.syncRoot(adapter.configRoot); err != nil {
		return ErrStorage
	}
	return nil
}

func configTemporaryName(operationID string) string {
	return "." + operationID + ".config"
}

func (adapter *Adapter) readTargetRaw() ([]byte, fileIdentity, error) {
	file, err := adapter.configRoot.OpenFile(adapter.configName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fileIdentity{}, ErrUnsafeState
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !safeConfigMode(info.Mode()) || !ownedByEffectiveUser(info) || linkCount(info) != 1 || info.Size() < 1 {
		return nil, fileIdentity{}, ErrUnsafeState
	}
	if info.Size() > MaxConfigBytes {
		return nil, fileIdentity{}, ErrLimit
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fileIdentity{}, ErrUnsafeState
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxConfigBytes+1))
	if err != nil {
		return nil, fileIdentity{}, ErrStorage
	}
	if len(content) > MaxConfigBytes {
		return nil, fileIdentity{}, ErrLimit
	}
	return content, fileIdentity{mode: info.Mode().Perm(), device: stat.Dev, inode: stat.Ino, uid: int(stat.Uid), gid: int(stat.Gid)}, nil
}

func normalizeMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return ErrRecoveryRequired
	}
	return err
}
