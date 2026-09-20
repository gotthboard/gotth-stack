package controller

import (
	"context"
	"encoding/json"

	"github.com/gotthboard/gotth-stack/internal/adapters/mailruntime"
)

type mailMechanism interface {
	Observe(context.Context, string) (mailruntime.Observation, error)
	StopPrevious(context.Context, string) (mailruntime.Summary, error)
	RenamePrevious(context.Context, string) (mailruntime.Summary, error)
	CreateCandidate(context.Context, string) (mailruntime.Summary, error)
	StartCandidate(context.Context, string) (mailruntime.Summary, error)
	StopCandidate(context.Context, string) (mailruntime.Summary, error)
	RemoveCandidate(context.Context, string) (mailruntime.Summary, error)
	RestorePreviousName(context.Context, string) (mailruntime.Summary, error)
	StartPrevious(context.Context, string) (mailruntime.Summary, error)
	VerifyCandidate(context.Context, string) (mailruntime.Summary, error)
	VerifyPrevious(context.Context, string) (mailruntime.Summary, error)
	ReconcileStage(string) (mailruntime.Summary, mailruntime.State, error)
}

func BindMailControlPlane(componentID string, adapter *mailruntime.ControlPlane, request mailruntime.ControlPlaneRequest) (*Binding, error) {
	if adapter == nil {
		return nil, ErrInvalidInput
	}
	return bindMail(componentID, AdapterMailControlPlane, []string{"runtime.replace"}, []string{"database-url", "extension-secrets", "front-auth-token", "master-key", "oidc-client-secret", "postfix-helper-token", "postfix-release-token"}, adapter, request.Candidate, func(ctx context.Context, operationID string) (*mailruntime.Prepared, mailruntime.Summary, error) {
		value := request
		value.Candidate.OperationID = operationID
		return adapter.Preflight(ctx, value)
	})
}

func BindMailFront(componentID string, adapter *mailruntime.Front, request mailruntime.FrontRequest) (*Binding, error) {
	if adapter == nil {
		return nil, ErrInvalidInput
	}
	return bindMail(componentID, AdapterMailFront, []string{"listener.public-mail", "runtime.replace"}, []string{"certificate", "front-auth-token", "private-key"}, adapter, request.Candidate, func(ctx context.Context, operationID string) (*mailruntime.Prepared, mailruntime.Summary, error) {
		value := request
		value.Candidate.OperationID = operationID
		return adapter.Preflight(ctx, value)
	})
}

func BindMailPostfix(componentID string, adapter *mailruntime.Postfix, request mailruntime.PostfixRequest) (*Binding, error) {
	if adapter == nil {
		return nil, ErrInvalidInput
	}
	return bindMail(componentID, AdapterMailPostfix, []string{"runtime.replace"}, []string{"helper-token", "release-token"}, adapter, request.Candidate, func(ctx context.Context, operationID string) (*mailruntime.Prepared, mailruntime.Summary, error) {
		value := request
		value.Candidate.OperationID = operationID
		return adapter.Preflight(ctx, value)
	})
}

func BindMailDovecot(componentID string, adapter *mailruntime.Dovecot, request mailruntime.DovecotRequest) (*Binding, error) {
	if adapter == nil {
		return nil, ErrInvalidInput
	}
	return bindMail(componentID, AdapterMailDovecot, []string{"runtime.replace"}, []string{"control-token"}, adapter, request.Candidate, func(ctx context.Context, operationID string) (*mailruntime.Prepared, mailruntime.Summary, error) {
		value := request
		value.Candidate.OperationID = operationID
		return adapter.Preflight(ctx, value)
	})
}

func BindMailRspamd(componentID string, adapter *mailruntime.Rspamd, request mailruntime.RspamdRequest) (*Binding, error) {
	if adapter == nil {
		return nil, ErrInvalidInput
	}
	return bindMail(componentID, AdapterMailRspamd, []string{"runtime.replace"}, []string{"controller-token", "dkim"}, adapter, request.Candidate, func(ctx context.Context, operationID string) (*mailruntime.Prepared, mailruntime.Summary, error) {
		value := request
		value.Candidate.OperationID = operationID
		return adapter.Preflight(ctx, value)
	})
}

func bindMail(componentID, adapterID string, capabilities, secretSlots []string, adapter mailMechanism, candidate mailruntime.Candidate, prepare func(context.Context, string) (*mailruntime.Prepared, mailruntime.Summary, error)) (*Binding, error) {
	if componentID == "" || candidate.ComponentID != componentID || candidate.OperationID != "" || candidate.SecretRevisionDigest == "" {
		return nil, ErrInvalidInput
	}
	observe := func(ctx context.Context, operationID string) ([]byte, error) {
		value, err := adapter.Observe(ctx, operationID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	decode := func(value []byte) (mailruntime.Observation, bool) {
		var observation mailruntime.Observation
		err := json.Unmarshal(value, &observation)
		return observation, err == nil
	}
	previousStopped := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != mailruntime.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == mailruntime.StateAbsent && observation.RollbackState == mailruntime.StateAbsent
		}
		return observation.PrimaryState == mailruntime.StatePrevious && observation.PrimaryPower == mailruntime.PowerStopped && observation.RollbackState == mailruntime.StateAbsent
	}
	preserved := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != mailruntime.StateCandidate {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == mailruntime.StateAbsent && observation.RollbackState == mailruntime.StateAbsent
		}
		return observation.PrimaryState == mailruntime.StateAbsent && observation.RollbackState == mailruntime.StatePrevious && observation.RollbackPower == mailruntime.PowerStopped
	}
	candidateStopped := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.PrimaryState != mailruntime.StateCandidate || observation.PrimaryPower != mailruntime.PowerStopped {
			return false
		}
		if observation.Summary.HadPrevious {
			return observation.RollbackState == mailruntime.StatePrevious && observation.RollbackPower == mailruntime.PowerStopped
		}
		return observation.RollbackState == mailruntime.StateAbsent
	}
	candidateRunning := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.PrimaryState != mailruntime.StateCandidate || observation.PrimaryPower != mailruntime.PowerRunning {
			return false
		}
		if observation.Summary.HadPrevious {
			return observation.RollbackState == mailruntime.StatePrevious && observation.RollbackPower == mailruntime.PowerStopped
		}
		return observation.RollbackState == mailruntime.StateAbsent && observation.RollbackPower == mailruntime.PowerAbsent
	}
	previousRunning := func(value []byte) bool {
		observation, ok := decode(value)
		if !ok || observation.StageState != mailruntime.StateCandidate || observation.RollbackState != mailruntime.StateAbsent {
			return false
		}
		if !observation.Summary.HadPrevious {
			return observation.PrimaryState == mailruntime.StateAbsent
		}
		power := mailruntime.PowerStopped
		if observation.Summary.PreviousRunning {
			power = mailruntime.PowerRunning
		}
		return observation.PrimaryState == mailruntime.StatePrevious && observation.PrimaryPower == power
	}
	secretDigests := make(map[string]string, len(secretSlots))
	for _, slot := range secretSlots {
		secretDigests[slot] = candidate.SecretRevisionDigest
	}
	var prepared *mailruntime.Prepared
	return &Binding{
		componentID: componentID, adapterID: adapterID, capabilities: capabilities, secretSlots: secretSlots, secretDigests: secretDigests,
		observe: observe,
		preflight: func(ctx context.Context, operationID string) ([]byte, error) {
			candidate, summary, err := prepare(ctx, operationID)
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
			_, err := prepared.Stage()
			prepared = nil
			return err
		},
		reconcileStage: func(_ context.Context, operationID string) error {
			_, state, err := adapter.ReconcileStage(operationID)
			if err == nil && state != mailruntime.StateCandidate {
				return ErrRecoveryRequired
			}
			return err
		},
		stageReached: func(value []byte) bool {
			observation, ok := decode(value)
			return ok && observation.StageState == mailruntime.StateCandidate
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
				_, err := adapter.StartPrevious(ctx, operationID)
				return err
			}, reached: previousStopped, predecessor: previousRunning, reversed: previousRunning},
			{name: "preserve-previous", call: func(ctx context.Context, operationID string) error {
				_, err := adapter.RenamePrevious(ctx, operationID)
				return err
			}, reverse: func(ctx context.Context, operationID string) error {
				_, err := adapter.RestorePreviousName(ctx, operationID)
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
			_, err := adapter.VerifyCandidate(ctx, operationID)
			return err
		}, reached: candidateRunning},
		verifyPrevious: transition{name: "verify-previous", call: func(ctx context.Context, operationID string) error {
			_, err := adapter.VerifyPrevious(ctx, operationID)
			return err
		}, reached: previousRunning},
	}, nil
}
