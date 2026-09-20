package controller

import (
	"context"
	"encoding/json"

	"github.com/gotthboard/gotth-stack/internal/adapters/authentik"
)

func BindAuthentik(componentID string, adapter *authentik.Adapter, request authentik.Request) (*Binding, error) {
	if adapter == nil || componentID == "" || request.ComponentID != componentID || request.OperationID != "" || request.DatabaseSecretRevisionDigest == "" || request.KeySecretRevisionDigest == "" {
		return nil, ErrInvalidInput
	}
	observe := func(ctx context.Context, operationID string) ([]byte, error) {
		value, err := adapter.Observe(ctx, operationID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	decode := func(value []byte) (authentik.Observation, bool) {
		var observation authentik.Observation
		err := json.Unmarshal(value, &observation)
		return observation, err == nil
	}
	absent := func(role authentik.RoleObservation) bool {
		return role.PrimaryState == authentik.StateAbsent && role.PrimaryPower == authentik.PowerAbsent && role.RollbackState == authentik.StateAbsent && role.RollbackPower == authentik.PowerAbsent
	}
	previous := func(role authentik.RoleObservation, power authentik.Power) bool {
		return role.PrimaryState == authentik.StatePrevious && role.PrimaryPower == power && role.RollbackState == authentik.StateAbsent && role.RollbackPower == authentik.PowerAbsent
	}
	preserved := func(role authentik.RoleObservation) bool {
		return role.PrimaryState == authentik.StateAbsent && role.PrimaryPower == authentik.PowerAbsent && role.RollbackState == authentik.StatePrevious && role.RollbackPower == authentik.PowerStopped
	}
	candidate := func(role authentik.RoleObservation, power authentik.Power, hadPrevious bool) bool {
		if role.PrimaryState != authentik.StateCandidate || role.PrimaryPower != power {
			return false
		}
		if hadPrevious {
			return role.RollbackState == authentik.StatePrevious && role.RollbackPower == authentik.PowerStopped
		}
		return role.RollbackState == authentik.StateAbsent && role.RollbackPower == authentik.PowerAbsent
	}
	initial := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != authentik.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return absent(observation.Server) && absent(observation.Worker)
		}
		serverPower, workerPower := authentik.PowerStopped, authentik.PowerStopped
		if observation.Summary.PreviousServerRunning {
			serverPower = authentik.PowerRunning
		}
		if observation.Summary.PreviousWorkerRunning {
			workerPower = authentik.PowerRunning
		}
		return previous(observation.Server, serverPower) && previous(observation.Worker, workerPower)
	}
	stopWorker := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != authentik.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return absent(observation.Server) && absent(observation.Worker)
		}
		serverPower := authentik.PowerStopped
		if observation.Summary.PreviousServerRunning {
			serverPower = authentik.PowerRunning
		}
		return previous(observation.Server, serverPower) && previous(observation.Worker, authentik.PowerStopped)
	}
	stopServer := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != authentik.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return absent(observation.Server) && absent(observation.Worker)
		}
		return previous(observation.Server, authentik.PowerStopped) && previous(observation.Worker, authentik.PowerStopped)
	}
	preserveWorker := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != authentik.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return absent(observation.Server) && absent(observation.Worker)
		}
		return previous(observation.Server, authentik.PowerStopped) && preserved(observation.Worker)
	}
	preserveServer := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != authentik.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return absent(observation.Server) && absent(observation.Worker)
		}
		return preserved(observation.Server) && preserved(observation.Worker)
	}
	createServer := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == authentik.StateCandidate && candidate(observation.Server, authentik.PowerStopped, observation.Summary.HadPrevious) && func() bool {
			if observation.Summary.HadPrevious {
				return preserved(observation.Worker)
			}
			return absent(observation.Worker)
		}()
	}
	createWorker := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == authentik.StateCandidate && candidate(observation.Server, authentik.PowerStopped, observation.Summary.HadPrevious) && candidate(observation.Worker, authentik.PowerStopped, observation.Summary.HadPrevious)
	}
	startServer := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == authentik.StateCandidate && candidate(observation.Server, authentik.PowerRunning, observation.Summary.HadPrevious) && candidate(observation.Worker, authentik.PowerStopped, observation.Summary.HadPrevious)
	}
	startWorker := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == authentik.StateCandidate && candidate(observation.Server, authentik.PowerRunning, observation.Summary.HadPrevious) && candidate(observation.Worker, authentik.PowerRunning, observation.Summary.HadPrevious)
	}
	call := func(action func(context.Context, string, authentik.Role) (authentik.Summary, error), role authentik.Role) func(context.Context, string) error {
		return func(ctx context.Context, operationID string) error {
			_, err := action(ctx, operationID, role)
			return err
		}
	}
	var prepared *authentik.Prepared
	return &Binding{
		componentID: componentID, adapterID: AdapterAuthentik,
		capabilities: []string{"runtime.replace"}, secretSlots: []string{"database-password", "secret-key"},
		secretDigests:  map[string]string{"database-password": request.DatabaseSecretRevisionDigest, "secret-key": request.KeySecretRevisionDigest},
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
			if err == nil && state != authentik.StateCandidate {
				return ErrRecoveryRequired
			}
			return err
		},
		stageReached: func(value []byte) bool {
			observation, ok := decode(value)
			return ok && observation.StageState == authentik.StateCandidate
		},
		rollbackReference: func(value []byte) (string, error) {
			observation, ok := decode(value)
			if !ok || observation.Summary.RollbackReference == "" {
				return "", ErrInvalidInput
			}
			return observation.Summary.RollbackReference, nil
		},
		forward: []transition{
			{name: "stop-worker", call: call(adapter.StopPrevious, authentik.RoleWorker), reverse: call(adapter.RestartPrevious, authentik.RoleWorker), reached: stopWorker, predecessor: initial, reversed: initial},
			{name: "stop-server", call: call(adapter.StopPrevious, authentik.RoleServer), reverse: call(adapter.RestartPrevious, authentik.RoleServer), reached: stopServer, predecessor: stopWorker, reversed: stopWorker},
			{name: "preserve-worker", call: call(adapter.PreservePrevious, authentik.RoleWorker), reverse: call(adapter.RestorePrevious, authentik.RoleWorker), reached: preserveWorker, predecessor: stopServer, reversed: stopServer},
			{name: "preserve-server", call: call(adapter.PreservePrevious, authentik.RoleServer), reverse: call(adapter.RestorePrevious, authentik.RoleServer), reached: preserveServer, predecessor: preserveWorker, reversed: preserveWorker},
			{name: "create-server", call: call(adapter.CreateCandidate, authentik.RoleServer), reverse: call(adapter.RemoveCandidate, authentik.RoleServer), reached: createServer, predecessor: preserveServer, reversed: preserveServer},
			{name: "create-worker", call: call(adapter.CreateCandidate, authentik.RoleWorker), reverse: call(adapter.RemoveCandidate, authentik.RoleWorker), reached: createWorker, predecessor: createServer, reversed: createServer},
			{name: "start-server", call: call(adapter.StartCandidate, authentik.RoleServer), reverse: call(adapter.StopCandidate, authentik.RoleServer), reached: startServer, predecessor: createWorker, reversed: createWorker},
			{name: "start-worker", call: call(adapter.StartCandidate, authentik.RoleWorker), reverse: call(adapter.StopCandidate, authentik.RoleWorker), reached: startWorker, predecessor: startServer, reversed: startServer},
		},
		rollbackOrder: []int{7, 6, 5, 4, 3, 2, 1, 0},
		verifyCandidate: transition{name: "verify-candidate", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, authentik.DesiredCandidate)
			return err
		}, reached: startWorker},
		verifyPrevious: transition{name: "verify-previous", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, authentik.DesiredPrevious)
			return err
		}, reached: initial},
	}, nil
}
