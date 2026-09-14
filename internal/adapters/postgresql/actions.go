package postgresql

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
)

func (adapter *Adapter) Observe(ctx context.Context, operationID string) (Observation, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Observation{}, ErrClosed
	}
	if !validID(operationID) {
		return Observation{}, ErrInvalidInput
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Observation{}, err
	}
	defer cancel()
	metadata, err := adapter.loadTransaction(operationID)
	if errors.Is(err, os.ErrNotExist) {
		if _, stageErr := adapter.transactionsRoot.Lstat(stageName(operationID)); stageErr == nil {
			return Observation{StageState: StateIncomplete, PrimaryState: StateAbsent, PrimaryPower: PowerAbsent, RollbackState: StateAbsent, RollbackPower: PowerAbsent, DataState: StateAbsent}, nil
		} else if !errors.Is(stageErr, os.ErrNotExist) {
			return Observation{}, ErrStorage
		}
		return Observation{StageState: StateAbsent, PrimaryState: StateAbsent, PrimaryPower: PowerAbsent, RollbackState: StateAbsent, RollbackPower: PowerAbsent, DataState: StateAbsent}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	primary, rollback, err := adapter.runtimePair(bounded, metadata)
	if err != nil {
		return Observation{}, err
	}
	dataState := StateOther
	if identity, initialized, dataErr := adapter.readDataIdentity(metadata.Candidate); dataErr == nil && dataIdentityMatches(metadata.DataIdentity, identity) {
		if initialized {
			if metadata.DataInitialized {
				dataState = StatePrevious
			} else {
				dataState = StateCandidate
			}
		} else {
			dataState = StatePrevious
		}
	}
	return Observation{
		Summary: summary(metadata), StageState: StateCandidate,
		PrimaryState: classifyRecord(primary, metadata), PrimaryPower: recordPower(primary),
		RollbackState: classifyRecord(rollback, metadata), RollbackPower: recordPower(rollback),
		DataState: dataState,
	}, nil
}

func (adapter *Adapter) StopPrevious(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if metadata.Previous == nil {
			if primary == nil && rollback == nil {
				return nil
			}
			return ErrRecoveryRequired
		}
		if rollback != nil && recordMatches(rollback, *metadata.Previous) && !rollback.running && primary == nil {
			return nil
		}
		if primary == nil || !recordMatches(primary, *metadata.Previous) || rollback != nil {
			return ErrRecoveryRequired
		}
		if !primary.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStop, name: metadata.Candidate.ContainerName, timeoutSec: int(adapter.timeout.Seconds())}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) PreservePrevious(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if metadata.Previous == nil {
			if primary == nil && rollback == nil {
				return nil
			}
			return ErrRecoveryRequired
		}
		if primary == nil && rollback != nil && recordMatches(rollback, *metadata.Previous) && !rollback.running {
			return nil
		}
		if primary == nil || primary.running || !recordMatches(primary, *metadata.Previous) || rollback != nil {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandRename, name: metadata.Candidate.ContainerName, destination: adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID)}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) CreateCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if primary != nil {
			if recordMatches(primary, metadata.Candidate) && !primary.running && validRollbackPosition(rollback, metadata) {
				return nil
			}
			return ErrRecoveryRequired
		}
		if !validRollbackPosition(rollback, metadata) {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{
			kind: commandCreate, name: metadata.Candidate.ContainerName, spec: metadata.Candidate,
			dataPath: adapter.absoluteDataPath(metadata.Candidate), secretPath: adapter.absoluteSecretPath(metadata.Candidate),
		}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) StartCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if primary == nil || !recordMatches(primary, metadata.Candidate) || !validRollbackPosition(rollback, metadata) {
			return ErrRecoveryRequired
		}
		if primary.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStart, name: metadata.Candidate.ContainerName}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) StopCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if primary == nil || !recordMatches(primary, metadata.Candidate) || !validRollbackPosition(rollback, metadata) {
			return ErrRecoveryRequired
		}
		if !primary.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStop, name: metadata.Candidate.ContainerName, timeoutSec: int(adapter.timeout.Seconds())}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RemoveCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if primary == nil {
			if validRollbackPosition(rollback, metadata) {
				return nil
			}
			return ErrRecoveryRequired
		}
		if primary.running || !recordMatches(primary, metadata.Candidate) || !validRollbackPosition(rollback, metadata) {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandRemove, name: metadata.Candidate.ContainerName}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RestorePrevious(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if metadata.Previous == nil {
			if primary == nil && rollback == nil {
				return nil
			}
			return ErrRecoveryRequired
		}
		if primary != nil && recordMatches(primary, *metadata.Previous) && !primary.running && rollback == nil {
			return nil
		}
		if primary != nil || rollback == nil || rollback.running || !recordMatches(rollback, *metadata.Previous) {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandRename, name: adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID), destination: metadata.Candidate.ContainerName}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RestartPrevious(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if metadata.Previous == nil {
			if primary == nil && rollback == nil {
				return nil
			}
			return ErrRecoveryRequired
		}
		if primary == nil || !recordMatches(primary, *metadata.Previous) || rollback != nil {
			return ErrRecoveryRequired
		}
		if metadata.PreviousRunning == primary.running {
			return nil
		}
		if !metadata.PreviousRunning {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStart, name: metadata.Candidate.ContainerName}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) Verify(ctx context.Context, operationID string, desired DesiredState) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	metadata, err := adapter.loadTransaction(operationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, ErrRecoveryRequired
		}
		return Summary{}, err
	}
	if err := adapter.validateBindings(bounded, metadata); err != nil {
		return Summary{}, err
	}
	primary, rollback, err := adapter.runtimePair(bounded, metadata)
	if err != nil {
		return Summary{}, err
	}
	switch desired {
	case DesiredCandidate:
		if primary == nil || !recordMatches(primary, metadata.Candidate) || !primary.running || !validRollbackPosition(rollback, metadata) {
			return Summary{}, ErrRecoveryRequired
		}
		if err := adapter.probe(bounded, metadata.Candidate); err != nil {
			return Summary{}, err
		}
	case DesiredPrevious:
		if metadata.Previous == nil {
			if primary != nil || rollback != nil {
				return Summary{}, ErrRecoveryRequired
			}
			return summary(metadata), nil
		}
		if primary == nil || !recordMatches(primary, *metadata.Previous) || primary.running != metadata.PreviousRunning || rollback != nil {
			return Summary{}, ErrRecoveryRequired
		}
		if primary.running {
			if err := adapter.probe(bounded, *metadata.Previous); err != nil {
				return Summary{}, err
			}
		}
	default:
		return Summary{}, ErrInvalidInput
	}
	return summary(metadata), nil
}

func (adapter *Adapter) mutate(ctx context.Context, operationID string, action func(context.Context, transactionMetadata, *containerRecord, *containerRecord) error) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	metadata, err := adapter.loadTransaction(operationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, ErrRecoveryRequired
		}
		return Summary{}, err
	}
	if err := adapter.validateBindings(bounded, metadata); err != nil {
		return Summary{}, err
	}
	primary, rollback, err := adapter.runtimePair(bounded, metadata)
	if err != nil {
		return Summary{}, err
	}
	if err := action(bounded, metadata, primary, rollback); err != nil {
		return Summary{}, err
	}
	return summary(metadata), nil
}

func (adapter *Adapter) validateBindings(ctx context.Context, metadata transactionMetadata) error {
	engine, err := adapter.engineDigest(ctx)
	if err != nil || engine != metadata.EngineDigest {
		return ErrConflict
	}
	dataIdentity, initialized, err := adapter.readDataIdentity(metadata.Candidate)
	if err != nil || !dataIdentityMatches(metadata.DataIdentity, dataIdentity) || metadata.DataInitialized && !initialized {
		return ErrConflict
	}
	secretIdentity, err := adapter.readSecretIdentity(metadata.Candidate)
	if err != nil || !identityMatches(metadata.SecretIdentity, secretIdentity) {
		return ErrConflict
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

func (adapter *Adapter) runtimePair(ctx context.Context, metadata transactionMetadata) (*containerRecord, *containerRecord, error) {
	primary, err := adapter.inspectContainer(ctx, metadata.Candidate.ContainerName)
	if err != nil {
		return nil, nil, err
	}
	rollback, err := adapter.inspectContainer(ctx, adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID))
	if err != nil {
		return nil, nil, err
	}
	return primary, rollback, nil
}

func classifyRecord(record *containerRecord, metadata transactionMetadata) State {
	if record == nil {
		return StateAbsent
	}
	if recordMatches(record, metadata.Candidate) {
		return StateCandidate
	}
	if metadata.Previous != nil && recordMatches(record, *metadata.Previous) {
		return StatePrevious
	}
	return StateOther
}

func recordMatches(record *containerRecord, spec managedSpec) bool {
	return record != nil && reflect.DeepEqual(record.spec, spec)
}

func recordPower(record *containerRecord) Power {
	if record == nil {
		return PowerAbsent
	}
	if record.running {
		return PowerRunning
	}
	return PowerStopped
}

func validRollbackPosition(record *containerRecord, metadata transactionMetadata) bool {
	if metadata.Previous == nil {
		return record == nil
	}
	return record != nil && !record.running && recordMatches(record, *metadata.Previous)
}

func mutationResult(err error) error {
	if err != nil {
		return ErrRecoveryRequired
	}
	return nil
}

func (adapter *Adapter) probe(ctx context.Context, spec managedSpec) error {
	if _, err := adapter.run(ctx, commandRequest{kind: commandReady, name: spec.ContainerName, spec: spec}, false); err != nil {
		return ErrContainer
	}
	value, err := adapter.run(ctx, commandRequest{kind: commandIdentity, name: spec.ContainerName, spec: spec}, true)
	if err != nil {
		return ErrContainer
	}
	line := strings.TrimSuffix(string(value), "\n")
	if strings.Contains(line, "\n") {
		return ErrContainer
	}
	fields := strings.Split(line, "|")
	if len(fields) != 4 || fields[1] != spec.Specification.Database || fields[2] != spec.Specification.Role || fields[3] != "f" {
		return ErrContainer
	}
	version, err := strconv.Atoi(fields[0])
	if err != nil || version < 170000 || version >= 180000 {
		return ErrContainer
	}
	return nil
}
