package controller

import (
	"context"
	"encoding/json"

	"github.com/gotthboard/gotth-stack/internal/adapters/caddy"
)

func BindCaddy(componentID string, adapter *caddy.Adapter, request caddy.Request) (*Binding, error) {
	if adapter == nil || componentID == "" || request.ComponentID != componentID || request.OperationID != "" {
		return nil, ErrInvalidInput
	}
	observe := func(ctx context.Context, operationID string) ([]byte, error) {
		value, err := adapter.Observe(ctx, operationID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	var prepared *caddy.Prepared
	preflight := func(ctx context.Context, operationID string) ([]byte, error) {
		value := request
		value.OperationID = operationID
		candidate, summary, err := adapter.Preflight(ctx, value)
		if err != nil {
			return nil, err
		}
		prepared = candidate
		return json.Marshal(summary)
	}
	decode := func(value []byte) (caddy.Observation, bool) {
		var observation caddy.Observation
		err := json.Unmarshal(value, &observation)
		return observation, err == nil
	}
	predicate := func(file, runtime caddy.State) func([]byte) bool {
		return func(value []byte) bool {
			observation, ok := decode(value)
			return ok && observation.StageState == caddy.StatePresent && observation.FileState == file && observation.FileTemporary == caddy.StateAbsent && observation.RuntimeState == runtime
		}
	}
	fileCandidate := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == caddy.StatePresent && observation.FileState == caddy.StateCandidate && observation.FileTemporary == caddy.StateAbsent
	}
	filePrevious := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == caddy.StatePresent && observation.FileState == caddy.StatePrevious && observation.FileTemporary == caddy.StateAbsent
	}
	runtimeCandidate := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.StageState == caddy.StatePresent && observation.FileState == caddy.StateCandidate && observation.RuntimeState == caddy.StateCandidate
	}
	runtimePrevious := predicate(caddy.StatePrevious, caddy.StatePrevious)
	binding := &Binding{
		componentID: componentID, adapterID: AdapterCaddy,
		capabilities: []string{"configuration.replace", "runtime.reload"}, secretSlots: []string{}, secretDigests: map[string]string{},
		observe: observe, preflight: preflight,
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
			if err == nil && state != caddy.StatePresent {
				return ErrRecoveryRequired
			}
			return err
		},
		stageReached: func(value []byte) bool {
			observation, ok := decode(value)
			return ok && observation.StageState == caddy.StatePresent
		},
		rollbackReference: func(value []byte) (string, error) {
			observation, ok := decode(value)
			if !ok || observation.Summary.RollbackReference == "" {
				return "", ErrInvalidInput
			}
			return observation.Summary.RollbackReference, nil
		},
		forward: []transition{
			{name: "install", call: func(_ context.Context, operationID string) error { _, err := adapter.Install(operationID); return err }, reached: fileCandidate, predecessor: filePrevious, reverse: func(_ context.Context, operationID string) error { _, err := adapter.Restore(operationID); return err }, reversed: filePrevious},
			{name: "activate", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.Activate(ctx, operationID)
				return err
			}, reached: runtimeCandidate, predecessor: func(value []byte) bool {
				observation, ok := decode(value)
				return ok && observation.RuntimeState == caddy.StatePrevious
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.Reactivate(ctx, operationID)
				return err
			}, reversed: runtimePrevious},
		},
		rollbackOrder: []int{0, 1},
		verifyCandidate: transition{name: "verify-candidate", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, caddy.DesiredCandidate)
			return err
		}, reached: predicate(caddy.StateCandidate, caddy.StateCandidate)},
		verifyPrevious: transition{name: "verify-previous", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.Verify(ctx, operationID, caddy.DesiredPrevious)
			return err
		}, reached: runtimePrevious},
	}
	return binding, nil
}
