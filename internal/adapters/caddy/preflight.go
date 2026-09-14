package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func (adapter *Adapter) Preflight(ctx context.Context, request Request) (*Prepared, Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil, Summary{}, ErrClosed
	}
	if !validID(request.OperationID) || !validID(request.ComponentID) || !digestPattern.MatchString(request.ExpectedDigest) {
		return nil, Summary{}, ErrInvalidInput
	}
	ctx, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	defer cancel()
	if err := validateConfiguration(request.Configuration); err != nil {
		return nil, Summary{}, err
	}
	candidate := append([]byte(nil), request.Configuration...)
	if digest(candidate) != request.ExpectedDigest {
		return nil, Summary{}, ErrConflict
	}
	previous, identity, err := adapter.readTarget()
	if err != nil {
		return nil, Summary{}, err
	}
	scratch, err := os.MkdirTemp(filepath.Join(adapter.statePath, scratchDirName), "preflight-")
	if err != nil {
		return nil, Summary{}, ErrStorage
	}
	if err := os.Chmod(scratch, 0o700); err != nil {
		_ = os.RemoveAll(scratch)
		return nil, Summary{}, ErrStorage
	}
	cleaned := false
	defer func() {
		if !cleaned {
			_ = os.RemoveAll(scratch)
		}
	}()
	previousPath := filepath.Join(scratch, "previous.caddyfile")
	candidatePath := filepath.Join(scratch, "candidate.caddyfile")
	if err := writeScratch(previousPath, previous); err != nil {
		return nil, Summary{}, err
	}
	if err := writeScratch(candidatePath, candidate); err != nil {
		return nil, Summary{}, err
	}
	environment := []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + scratch,
		"XDG_CONFIG_HOME=" + filepath.Join(scratch, "config"),
		"XDG_DATA_HOME=" + filepath.Join(scratch, "data"),
	}
	previousRuntime, err := adapter.adapt(ctx, previousPath, environment)
	if err != nil {
		return nil, Summary{}, err
	}
	candidateRuntime, err := adapter.adapt(ctx, candidatePath, environment)
	if err != nil {
		return nil, Summary{}, err
	}
	if !adminBound(previousRuntime, adapter.adminAddress) {
		return nil, Summary{}, ErrConflict
	}
	if !adminBound(candidateRuntime, adapter.adminAddress) {
		return nil, Summary{}, ErrValidation
	}
	if _, err := adapter.run(ctx, commandRequest{kind: commandValidate, configPath: candidatePath, workingDirectory: adapter.workingDirectory, environment: environment}, false); err != nil {
		if errors.Is(err, ErrCommand) {
			err = ErrValidation
		}
		return nil, Summary{}, err
	}
	running, err := adapter.runtime.Read(ctx)
	if err != nil {
		return nil, Summary{}, err
	}
	previousRuntimeDigest := digest(previousRuntime)
	if digest(running) != previousRuntimeDigest {
		return nil, Summary{}, ErrConflict
	}
	metadata := transactionMetadata{
		SchemaVersion: SchemaVersion, OperationID: request.OperationID, ComponentID: request.ComponentID,
		BindingDigest: adapter.bindingDigest(), PreviousFileDigest: digest(previous),
		CandidateFileDigest: digest(candidate), PreviousRuntimeDigest: previousRuntimeDigest,
		CandidateRuntimeDigest: digest(candidateRuntime), ConfigMode: uint32(identity.mode.Perm()),
		ConfigUID: uint32(identity.uid), ConfigGID: uint32(identity.gid),
	}
	unsigned, err := json.Marshal(metadata)
	if err != nil {
		return nil, Summary{}, ErrStorage
	}
	metadata.RollbackReference = digest(unsigned)
	prepared := &Prepared{
		adapter: adapter, metadata: metadata, previous: previous, candidate: candidate, target: identity,
	}
	if err := os.RemoveAll(scratch); err != nil {
		return nil, Summary{}, ErrStorage
	}
	cleaned = true
	return prepared, summary(metadata), nil
}

func (adapter *Adapter) adapt(ctx context.Context, path string, environment []string) ([]byte, error) {
	value, err := adapter.run(ctx, commandRequest{kind: commandAdapt, configPath: path, workingDirectory: adapter.workingDirectory, environment: environment}, true)
	if err != nil {
		if errors.Is(err, ErrCommand) {
			return nil, ErrValidation
		}
		return nil, err
	}
	canonical, err := canonicalJSON(value)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return nil, err
		}
		return nil, ErrValidation
	}
	return canonical, nil
}

func writeScratch(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(value)); err != nil {
		return ErrStorage
	}
	closed = true
	if err := file.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

func (adapter *Adapter) bindingDigest() string {
	return digest([]byte(adapter.binary + "\x00" + adapter.binaryDigest + "\x00" + adapter.configPath + "\x00" + adapter.statePath + "\x00" + adapter.workingDirectory + "\x00" + adapter.adminURL))
}
