package authentik

import (
	"context"
	"errors"
	"os"
)

type roleRuntime struct{ primary, rollback *containerRecord }

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
			return Observation{StageState: StateIncomplete, Server: absentObservation(), Worker: absentObservation()}, nil
		} else if !errors.Is(stageErr, os.ErrNotExist) {
			return Observation{}, ErrStorage
		}
		return Observation{StageState: StateAbsent, Server: absentObservation(), Worker: absentObservation()}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	runtime, err := adapter.runtime(bounded, metadata)
	if err != nil {
		return Observation{}, err
	}
	return Observation{Summary: summary(metadata), StageState: StateCandidate, Server: observeRole(runtime[RoleServer], metadata, RoleServer), Worker: observeRole(runtime[RoleWorker], metadata, RoleWorker)}, nil
}

func absentObservation() RoleObservation {
	return RoleObservation{PrimaryState: StateAbsent, PrimaryPower: PowerAbsent, RollbackState: StateAbsent, RollbackPower: PowerAbsent}
}
func observeRole(runtime roleRuntime, metadata transactionMetadata, role Role) RoleObservation {
	return RoleObservation{PrimaryState: classify(runtime.primary, metadata, role), PrimaryPower: recordPower(runtime.primary), RollbackState: classify(runtime.rollback, metadata, role), RollbackPower: recordPower(runtime.rollback)}
}

func (adapter *Adapter) StopPrevious(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if m.Previous == nil {
			return requireAllAbsent(r)
		}
		if r[RoleServer].rollback != nil || r[RoleWorker].rollback != nil {
			return ErrRecoveryRequired
		}
		if role == RoleWorker {
			if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || !recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) {
				return ErrRecoveryRequired
			}
		} else {
			if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || !recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) || r[RoleWorker].primary.running {
				return ErrRecoveryRequired
			}
		}
		record := r[role].primary
		if !record.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStop, name: adapter.containerName(m.ComponentID, role), timeoutSec: int(adapter.timeout.Seconds())}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) PreservePrevious(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if m.Previous == nil {
			return requireAllAbsent(r)
		}
		if role == RoleWorker {
			if recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) && !r[RoleServer].primary.running && r[RoleServer].rollback == nil && r[RoleWorker].primary == nil && recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) && !r[RoleWorker].rollback.running {
				return nil
			}
			if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || r[RoleServer].primary.running || !recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) || r[RoleWorker].primary.running || r[RoleServer].rollback != nil || r[RoleWorker].rollback != nil {
				return ErrRecoveryRequired
			}
		} else {
			if r[RoleServer].primary == nil && recordMatches(r[RoleServer].rollback, *m.Previous, RoleServer) && !r[RoleServer].rollback.running && r[RoleWorker].primary == nil && recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) && !r[RoleWorker].rollback.running {
				return nil
			}
			if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || r[RoleServer].primary.running || r[RoleServer].rollback != nil || r[RoleWorker].primary != nil || !recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) || r[RoleWorker].rollback.running {
				return ErrRecoveryRequired
			}
		}
		name := adapter.containerName(m.ComponentID, role)
		_, err := adapter.run(ctx, commandRequest{kind: commandRename, name: name, destination: adapter.rollbackName(name, m.OperationID)}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) CreateCandidate(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if !validPreviousRollbacks(r, m) {
			return ErrRecoveryRequired
		}
		if role == RoleServer {
			if r[RoleWorker].primary != nil {
				return ErrRecoveryRequired
			}
		} else {
			if !recordMatches(r[RoleServer].primary, m.Candidate, RoleServer) || r[RoleServer].primary.running {
				return ErrRecoveryRequired
			}
		}
		if r[role].primary != nil {
			if recordMatches(r[role].primary, m.Candidate, role) && !r[role].primary.running {
				return nil
			}
			return ErrRecoveryRequired
		}
		data, certs, templates, database, key := adapter.paths(m.Candidate)
		_, err := adapter.run(ctx, commandRequest{kind: commandCreate, name: adapter.containerName(m.ComponentID, role), spec: m.Candidate, role: role, dataPath: data, certsPath: certs, templatesPath: templates, databaseSecretPath: database, keySecretPath: key}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) StartCandidate(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if !validPreviousRollbacks(r, m) || !recordMatches(r[RoleServer].primary, m.Candidate, RoleServer) || !recordMatches(r[RoleWorker].primary, m.Candidate, RoleWorker) {
			return ErrRecoveryRequired
		}
		if role == RoleServer {
			if r[RoleWorker].primary.running {
				return ErrRecoveryRequired
			}
		} else {
			if !r[RoleServer].primary.running {
				return ErrRecoveryRequired
			}
			if err := adapter.probe(ctx, m.Candidate, RoleServer); err != nil {
				return err
			}
		}
		if r[role].primary.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStart, name: adapter.containerName(m.ComponentID, role)}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) StopCandidate(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if !validPreviousRollbacks(r, m) || !recordMatches(r[RoleServer].primary, m.Candidate, RoleServer) || !recordMatches(r[RoleWorker].primary, m.Candidate, RoleWorker) {
			return ErrRecoveryRequired
		}
		if role == RoleServer && r[RoleWorker].primary.running {
			return ErrRecoveryRequired
		}
		if !r[role].primary.running {
			return nil
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStop, name: adapter.containerName(m.ComponentID, role), timeoutSec: int(adapter.timeout.Seconds())}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RemoveCandidate(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if !validPreviousRollbacks(r, m) {
			return ErrRecoveryRequired
		}
		if role == RoleWorker {
			if r[RoleServer].primary == nil || !recordMatches(r[RoleServer].primary, m.Candidate, RoleServer) || r[RoleServer].primary.running {
				return ErrRecoveryRequired
			}
		} else if r[RoleWorker].primary != nil {
			return ErrRecoveryRequired
		}
		record := r[role].primary
		if record == nil {
			return nil
		}
		if record.running || !recordMatches(record, m.Candidate, role) {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandRemove, name: adapter.containerName(m.ComponentID, role)}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RestorePrevious(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if m.Previous == nil {
			return requireAllAbsent(r)
		}
		if role == RoleServer {
			if recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) && !r[RoleServer].primary.running && r[RoleServer].rollback == nil && r[RoleWorker].primary == nil && recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) && !r[RoleWorker].rollback.running {
				return nil
			}
			if r[RoleServer].primary != nil || r[RoleWorker].primary != nil || !recordMatches(r[RoleServer].rollback, *m.Previous, RoleServer) || r[RoleServer].rollback.running || !recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) || r[RoleWorker].rollback.running {
				return ErrRecoveryRequired
			}
		} else {
			if recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) && !r[RoleServer].primary.running && r[RoleServer].rollback == nil && recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) && !r[RoleWorker].primary.running && r[RoleWorker].rollback == nil {
				return nil
			}
			if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || r[RoleServer].primary.running || r[RoleServer].rollback != nil || r[RoleWorker].primary != nil || !recordMatches(r[RoleWorker].rollback, *m.Previous, RoleWorker) || r[RoleWorker].rollback.running {
				return ErrRecoveryRequired
			}
		}
		name := adapter.containerName(m.ComponentID, role)
		_, err := adapter.run(ctx, commandRequest{kind: commandRename, name: adapter.rollbackName(name, m.OperationID), destination: name}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) RestartPrevious(ctx context.Context, operationID string, role Role) (Summary, error) {
	return adapter.mutate(ctx, operationID, role, func(ctx context.Context, m transactionMetadata, r map[Role]roleRuntime) error {
		if m.Previous == nil {
			return requireAllAbsent(r)
		}
		if !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || !recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) || r[RoleServer].rollback != nil || r[RoleWorker].rollback != nil {
			return ErrRecoveryRequired
		}
		wanted := m.PreviousServerRunning
		if role == RoleWorker {
			wanted = m.PreviousWorkerRunning
			if wanted {
				if !r[RoleServer].primary.running {
					return ErrRecoveryRequired
				}
				if err := adapter.probe(ctx, *m.Previous, RoleServer); err != nil {
					return err
				}
			}
		}
		if r[role].primary.running == wanted {
			return nil
		}
		if !wanted {
			return ErrRecoveryRequired
		}
		_, err := adapter.run(ctx, commandRequest{kind: commandStart, name: adapter.containerName(m.ComponentID, role)}, false)
		return mutationResult(err)
	})
}

func (adapter *Adapter) VerifyRole(ctx context.Context, operationID string, role Role, desired DesiredState) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	if !validRole(role) {
		return Summary{}, ErrInvalidInput
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	m, err := adapter.loadTransaction(operationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, ErrRecoveryRequired
		}
		return Summary{}, err
	}
	if err := adapter.validateBindings(bounded, m); err != nil {
		return Summary{}, err
	}
	r, err := adapter.runtime(bounded, m)
	if err != nil {
		return Summary{}, err
	}
	var spec managedSpec
	var running bool
	switch desired {
	case DesiredCandidate:
		spec = m.Candidate
		running = true
		if !validPreviousRollbacks(r, m) {
			return Summary{}, ErrRecoveryRequired
		}
	case DesiredPrevious:
		if m.Previous == nil {
			return Summary{}, ErrRecoveryRequired
		}
		spec = *m.Previous
		if role == RoleServer {
			running = m.PreviousServerRunning
		} else {
			running = m.PreviousWorkerRunning
		}
		if r[role].rollback != nil {
			return Summary{}, ErrRecoveryRequired
		}
	default:
		return Summary{}, ErrInvalidInput
	}
	if !recordMatches(r[role].primary, spec, role) || r[role].primary.running != running {
		return Summary{}, ErrRecoveryRequired
	}
	if running {
		if err := adapter.probe(bounded, spec, role); err != nil {
			return Summary{}, err
		}
	}
	return summary(m), nil
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
	m, err := adapter.loadTransaction(operationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, ErrRecoveryRequired
		}
		return Summary{}, err
	}
	if err := adapter.validateBindings(bounded, m); err != nil {
		return Summary{}, err
	}
	r, err := adapter.runtime(bounded, m)
	if err != nil {
		return Summary{}, err
	}
	if desired == DesiredCandidate {
		if !validPreviousRollbacks(r, m) || !recordMatches(r[RoleServer].primary, m.Candidate, RoleServer) || !recordMatches(r[RoleWorker].primary, m.Candidate, RoleWorker) || !r[RoleServer].primary.running || !r[RoleWorker].primary.running {
			return Summary{}, ErrRecoveryRequired
		}
		for _, role := range []Role{RoleServer, RoleWorker} {
			if err := adapter.probe(bounded, m.Candidate, role); err != nil {
				return Summary{}, err
			}
		}
	} else if desired == DesiredPrevious {
		if m.Previous == nil {
			if err := requireAllAbsent(r); err != nil {
				return Summary{}, err
			}
			return summary(m), nil
		}
		if r[RoleServer].rollback != nil || r[RoleWorker].rollback != nil || !recordMatches(r[RoleServer].primary, *m.Previous, RoleServer) || !recordMatches(r[RoleWorker].primary, *m.Previous, RoleWorker) || r[RoleServer].primary.running != m.PreviousServerRunning || r[RoleWorker].primary.running != m.PreviousWorkerRunning {
			return Summary{}, ErrRecoveryRequired
		}
		if m.PreviousServerRunning {
			if err := adapter.probe(bounded, *m.Previous, RoleServer); err != nil {
				return Summary{}, err
			}
		}
		if m.PreviousWorkerRunning {
			if err := adapter.probe(bounded, *m.Previous, RoleWorker); err != nil {
				return Summary{}, err
			}
		}
	} else {
		return Summary{}, ErrInvalidInput
	}
	return summary(m), nil
}

func (adapter *Adapter) mutate(ctx context.Context, operationID string, role Role, action func(context.Context, transactionMetadata, map[Role]roleRuntime) error) (Summary, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return Summary{}, ErrClosed
	}
	if !validRole(role) {
		return Summary{}, ErrInvalidInput
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer cancel()
	m, err := adapter.loadTransaction(operationID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, ErrRecoveryRequired
		}
		return Summary{}, err
	}
	if err := adapter.validateBindings(bounded, m); err != nil {
		return Summary{}, err
	}
	runtime, err := adapter.runtime(bounded, m)
	if err != nil {
		return Summary{}, err
	}
	if err := action(bounded, m, runtime); err != nil {
		return Summary{}, err
	}
	return summary(m), nil
}

func (adapter *Adapter) validateBindings(ctx context.Context, m transactionMetadata) error {
	engine, err := adapter.engineDigest(ctx)
	if err != nil || engine != m.EngineDigest {
		return ErrConflict
	}
	if err := adapter.validateFileBindings(m); err != nil {
		return err
	}
	candidateID, err := adapter.inspectImage(ctx, m.Candidate)
	if err != nil || candidateID != m.Candidate.ImageID {
		return ErrImage
	}
	if m.Previous != nil {
		previousID, err := adapter.inspectImage(ctx, *m.Previous)
		if err != nil || previousID != m.Previous.ImageID {
			return ErrImage
		}
	}
	return nil
}
func (adapter *Adapter) runtime(ctx context.Context, m transactionMetadata) (map[Role]roleRuntime, error) {
	result := make(map[Role]roleRuntime, 2)
	for _, role := range []Role{RoleServer, RoleWorker} {
		name := adapter.containerName(m.ComponentID, role)
		primary, err := adapter.inspectContainer(ctx, name)
		if err != nil {
			return nil, err
		}
		rollback, err := adapter.inspectContainer(ctx, adapter.rollbackName(name, m.OperationID))
		if err != nil {
			return nil, err
		}
		result[role] = roleRuntime{primary: primary, rollback: rollback}
	}
	return result, nil
}
func validPreviousRollbacks(r map[Role]roleRuntime, m transactionMetadata) bool {
	for _, role := range []Role{RoleServer, RoleWorker} {
		rollback := r[role].rollback
		if m.Previous == nil {
			if rollback != nil {
				return false
			}
		} else if !recordMatches(rollback, *m.Previous, role) || rollback.running {
			return false
		}
	}
	return true
}
func requireAllAbsent(r map[Role]roleRuntime) error {
	for _, role := range []Role{RoleServer, RoleWorker} {
		if r[role].primary != nil || r[role].rollback != nil {
			return ErrRecoveryRequired
		}
	}
	return nil
}
func classify(record *containerRecord, m transactionMetadata, role Role) State {
	if record == nil {
		return StateAbsent
	}
	if recordMatches(record, m.Candidate, role) {
		return StateCandidate
	}
	if m.Previous != nil && recordMatches(record, *m.Previous, role) {
		return StatePrevious
	}
	return StateOther
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
func mutationResult(err error) error {
	if err != nil {
		return ErrRecoveryRequired
	}
	return nil
}
func (adapter *Adapter) probe(ctx context.Context, spec managedSpec, role Role) error {
	if _, err := adapter.run(ctx, commandRequest{kind: commandHealth, name: adapter.containerName(spec.ComponentID, role), role: role}, false); err != nil {
		return ErrContainer
	}
	return nil
}
