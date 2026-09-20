package controller

import (
	"context"
	"encoding/json"

	"github.com/gotthboard/gotth-stack/internal/adapters/postgresql"
)

func BindPostgreSQL(componentID string, adapter *postgresql.Adapter, request postgresql.Request) (*Binding, error) {
	if adapter == nil || componentID == "" || request.ComponentID != componentID || request.OperationID != "" || request.SecretRevisionDigest == "" {
		return nil, ErrInvalidInput
	}
	observe := func(ctx context.Context, operationID string) ([]byte, error) {
		value, err := adapter.Observe(ctx, operationID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	decode := func(value []byte) (postgresql.Observation, bool) {
		var observation postgresql.Observation
		err := json.Unmarshal(value, &observation)
		return observation, err == nil
	}
	previousStopped := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != postgresql.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == postgresql.StateAbsent && observation.PrimaryPower == postgresql.PowerAbsent && observation.RollbackState == postgresql.StateAbsent && observation.RollbackPower == postgresql.PowerAbsent
		}
		return observation.PrimaryState == postgresql.StatePrevious && observation.PrimaryPower == postgresql.PowerStopped && observation.RollbackState == postgresql.StateAbsent && observation.RollbackPower == postgresql.PowerAbsent
	}
	preserved := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != postgresql.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == postgresql.StateAbsent && observation.PrimaryPower == postgresql.PowerAbsent && observation.RollbackState == postgresql.StateAbsent && observation.RollbackPower == postgresql.PowerAbsent
		}
		return observation.PrimaryState == postgresql.StateAbsent && observation.PrimaryPower == postgresql.PowerAbsent && observation.RollbackState == postgresql.StatePrevious && observation.RollbackPower == postgresql.PowerStopped
	}
	candidateStopped := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != postgresql.StateCandidate || observation.PrimaryState != postgresql.StateCandidate || observation.PrimaryPower != postgresql.PowerStopped {
			return false
		}
		if observation.Summary.HadPrevious {
			return observation.RollbackState == postgresql.StatePrevious && observation.RollbackPower == postgresql.PowerStopped
		}
		return observation.RollbackState == postgresql.StateAbsent && observation.RollbackPower == postgresql.PowerAbsent
	}
	candidateRunning := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != postgresql.StateCandidate || observation.PrimaryState != postgresql.StateCandidate || observation.PrimaryPower != postgresql.PowerRunning {
			return false
		}
		if observation.Summary.HadPrevious {
			return observation.RollbackState == postgresql.StatePrevious && observation.RollbackPower == postgresql.PowerStopped
		}
		return observation.RollbackState == postgresql.StateAbsent && observation.RollbackPower == postgresql.PowerAbsent
	}
	previousRunning := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != postgresql.StateCandidate || observation.RollbackState != postgresql.StateAbsent || observation.RollbackPower != postgresql.PowerAbsent {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == postgresql.StateAbsent && observation.PrimaryPower == postgresql.PowerAbsent
		}
		power := postgresql.PowerStopped
		if observation.Summary.PreviousRunning {
			power = postgresql.PowerRunning
		}
		return observation.PrimaryState == postgresql.StatePrevious && observation.PrimaryPower == power
	}
	var prepared *postgresql.Prepared
	return &Binding{
		componentID: componentID, adapterID: AdapterPostgreSQL,
		capabilities: []string{"runtime.replace"}, secretSlots: []string{"password"}, secretDigests: map[string]string{"password": request.SecretRevisionDigest},
		artifactDigest: imageArtifactDigest(request.Specification.Image), configurationDigest: request.ConfigurationDigest,
		observe: observe,
		preflight: func(ctx context.Context, operationID string) ([]byte, error) {
			value := request
			value.OperationID = operationID
			candidate, summary, err := adapter.Preflight(ctx, value)
			if err != nil {
				return nil, err
			}
			prepared = candidate
			return json.Marshal(summary)
		},
		stage: func(_ context.Context, _ string) error {
			if prepared == nil {
				return ErrRecoveryRequired
			}
			_, err := adapter.Stage(prepared)
			prepared = nil
			return err
		},
		reconcileStage: func(_ context.Context, operationID string) error {
			_, state, err := adapter.ReconcileStage(operationID)
			if err == nil && state != postgresql.StateCandidate {
				return ErrRecoveryRequired
			}
			return err
		},
		stageReached: func(value []byte) bool {
			observation, ok := decode(value)
			return ok && observation.StageState == postgresql.StateCandidate
		},
		rollbackReference: func(value []byte) (string, error) {
			observation, ok := decode(value)
			if !ok || observation.Summary.RollbackReference == "" {
				return "", ErrInvalidInput
			}
			return observation.Summary.RollbackReference, nil
		},
		forward: []transition{
			{name: "stop-previous", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.StopPrevious(ctx, operationID)
				return err
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.RestartPrevious(ctx, operationID)
				return err
			}, reached: previousStopped, predecessor: previousRunning, reversed: previousRunning},
			{name: "preserve-previous", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.PreservePrevious(ctx, operationID)
				return err
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.RestorePrevious(ctx, operationID)
				return err
			}, reached: preserved, predecessor: previousStopped, reversed: previousStopped},
			{name: "create-candidate", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.CreateCandidate(ctx, operationID)
				return err
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.RemoveCandidate(ctx, operationID)
				return err
			}, reached: candidateStopped, predecessor: preserved, reversed: preserved},
			{name: "start-candidate", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.StartCandidate(ctx, operationID)
				return err
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.StopCandidate(ctx, operationID)
				return err
			}, reached: candidateRunning, predecessor: candidateStopped, reversed: candidateStopped},
		},
		rollbackOrder: []int{3, 2, 1, 0},
		verifyCandidate: transition{name: "verify-candidate", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, postgresql.DesiredCandidate)
			return err
		}, reached: candidateRunning},
		verifyPrevious: transition{name: "verify-previous", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, postgresql.DesiredPrevious)
			return err
		}, reached: previousRunning},
	}, nil
}
