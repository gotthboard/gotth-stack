package mailruntime

import (
	"context"
	"errors"
	"os"
	"reflect"
)

func (adapter *adapter) Observe(ctx context.Context, operationID string) (Observation, error) {
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
		if _, stageErr := adapter.transactions.Lstat(stageName(operationID)); stageErr == nil {
			return Observation{StageState: StateIncomplete, PrimaryState: StateAbsent, PrimaryPower: PowerAbsent, RollbackState: StateAbsent, RollbackPower: PowerAbsent, BindingState: StateAbsent}, nil
		} else if !errors.Is(stageErr, os.ErrNotExist) {
			return Observation{}, ErrStorage
		}
		return Observation{StageState: StateAbsent, PrimaryState: StateAbsent, PrimaryPower: PowerAbsent, RollbackState: StateAbsent, RollbackPower: PowerAbsent, BindingState: StateAbsent}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	if err := adapter.validateBindings(bounded, metadata); err != nil {
		return Observation{}, err
	}
	primary, rollback, err := adapter.runtimePair(bounded, metadata)
	if err != nil {
		return Observation{}, err
	}
	return Observation{
		Summary: summary(metadata), StageState: StateCandidate,
		PrimaryState: classifyRecord(primary, metadata), PrimaryPower: recordPower(primary),
		RollbackState: classifyRecord(rollback, metadata), RollbackPower: recordPower(rollback), BindingState: StateCandidate,
	}, nil
}

func (adapter *adapter) StopPrevious(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) RenamePrevious(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) CreateCandidate(ctx context.Context, operationID string) (Summary, error) {
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
		_, err := adapter.run(ctx, commandRequest{kind: commandCreate, name: metadata.Candidate.ContainerName, spec: metadata.Candidate, definition: adapter.definition}, false)
		return mutationResult(err)
	})
}

func (adapter *adapter) StartCandidate(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) StopCandidate(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) RemoveCandidate(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) RestorePreviousName(ctx context.Context, operationID string) (Summary, error) {
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

func (adapter *adapter) StartPrevious(ctx context.Context, operationID string) (Summary, error) {
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
		if primary.running == metadata.PreviousRunning {
			return nil
		}
		if !metadata.PreviousRunning {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStart, name: metadata.Candidate.ContainerName}, false)
		return mutationResult(err)
	})
}

func (adapter *adapter) VerifyCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.verify(ctx, operationID, DesiredCandidate)
}

func (adapter *adapter) VerifyPrevious(ctx context.Context, operationID string) (Summary, error) {
	return adapter.verify(ctx, operationID, DesiredPrevious)
}

func (adapter *adapter) verify(ctx context.Context, operationID string, desired DesiredState) (Summary, error) {
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

func (adapter *adapter) CommitCandidate(ctx context.Context, operationID string) (Summary, error) {
	return adapter.mutate(ctx, operationID, func(ctx context.Context, metadata transactionMetadata, primary, rollback *containerRecord) error {
		if primary == nil || !recordMatches(primary, metadata.Candidate) || !primary.running {
			return ErrRecoveryRequired
		}
		if err := adapter.probe(ctx, metadata.Candidate); err != nil {
			return err
		}
		if rollback == nil {
			return nil
		}
		if rollback.running || metadata.Previous == nil || !recordMatches(rollback, *metadata.Previous) {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandRemove, name: adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID)}, false)
		return mutationResult(err)
	})
}

func (adapter *adapter) ReconcileStage(operationID string) (Summary, State, error) {
	return adapter.reconcileStage(operationID)
}

func (adapter *adapter) RollbackStage(ctx context.Context, operationID string) error {
	return adapter.rollbackStage(ctx, operationID)
}

func (adapter *adapter) mutate(ctx context.Context, operationID string, action func(context.Context, transactionMetadata, *containerRecord, *containerRecord) error) (Summary, error) {
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

func (adapter *adapter) runtimePair(ctx context.Context, metadata transactionMetadata) (*containerRecord, *containerRecord, error) {
	primary, err := adapter.inspectEither(ctx, metadata.Candidate.ContainerName, metadata.Candidate, metadata.Previous)
	if err != nil {
		return nil, nil, err
	}
	rollback, err := adapter.inspectContainer(ctx, adapter.rollbackName(metadata.Candidate.ContainerName, metadata.OperationID), metadata.Previous)
	if err != nil {
		return nil, nil, err
	}
	return primary, rollback, nil
}

func (adapter *adapter) inspectEither(ctx context.Context, name string, candidate managedSpec, previous *managedSpec) (*containerRecord, error) {
	record, err := adapter.inspectContainer(ctx, name, &candidate)
	if err == nil || previous == nil || !errors.Is(err, ErrContainer) {
		return record, err
	}
	return adapter.inspectContainer(ctx, name, previous)
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

func (adapter *adapter) probe(ctx context.Context, spec managedSpec) error {
	if _, err := adapter.run(ctx, commandRequest{kind: commandHealth, name: spec.ContainerName, definition: adapter.definition}, false); err != nil {
		return ErrContainer
	}
	return nil
}
