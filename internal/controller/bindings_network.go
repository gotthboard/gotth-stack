package controller

import (
	"context"
	"encoding/json"

	"github.com/gotthboard/gotth-stack/internal/adapters/mailruntime"
	"github.com/gotthboard/gotth-stack/pkg/journal"
)

type networkObservation struct {
	Exists  bool                       `json:"exists"`
	Summary mailruntime.NetworkSummary `json:"summary"`
}

// BindMailNetwork admits the one fixed mail-runtime network bootstrap. It is
// intentionally a separate operation from role replacement because network
// creation has no honest reverse action.
func BindMailNetwork(componentID, artifactDigest, configurationDigest string, adapter *mailruntime.ControlPlane) (*Binding, error) {
	if componentID == "" || artifactDigest == "" || configurationDigest == "" || adapter == nil {
		return nil, ErrInvalidInput
	}
	observe := func(ctx context.Context, _ string) ([]byte, error) {
		summary, exists, err := adapter.ObserveNetwork(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(networkObservation{Exists: exists, Summary: summary})
	}
	decode := func(value []byte) (networkObservation, bool) {
		var observation networkObservation
		err := json.Unmarshal(value, &observation)
		return observation, err == nil
	}
	present := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && observation.Exists && observation.Summary.Name != "" && observation.Summary.ID != "" && observation.Summary.Digest != ""
	}
	absent := func(value []byte) bool {
		observation, ok := decode(value)
		return ok && !observation.Exists && observation.Summary == (mailruntime.NetworkSummary{})
	}
	verify := func(ctx context.Context, _ string) error {
		_, exists, err := adapter.ObserveNetwork(ctx)
		if err == nil && !exists {
			return ErrAdapter
		}
		return err
	}
	return &Binding{
		componentID: componentID, adapterID: AdapterMailControlPlane,
		capabilities: []string{"network.ensure"}, secretSlots: []string{}, secretDigests: map[string]string{},
		artifactDigest: artifactDigest, configurationDigest: configurationDigest,
		observe:           observe,
		preflight:         func(ctx context.Context, operationID string) ([]byte, error) { return observe(ctx, operationID) },
		stage:             func(context.Context, string) error { return nil },
		reconcileStage:    func(context.Context, string) error { return nil },
		stageReached:      func([]byte) bool { return true },
		rollbackReference: func([]byte) (string, error) { return digest([]byte(AdapterMailControlPlane + "\x00network")), nil },
		forward: []transition{{
			name: "ensure-network", call: func(ctx context.Context, _ string) error { _, err := adapter.EnsureNetwork(ctx); return err },
			reached: present, predecessor: absent, recoveryOnly: journal.RecoveryNoRollback,
		}},
		rollbackOrder:   []int{},
		verifyCandidate: transition{name: "verify-network", call: verify, reached: present},
		verifyPrevious:  transition{name: "verify-network", call: verify, reached: present},
	}, nil
}
