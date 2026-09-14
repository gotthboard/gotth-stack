package journal

import "time"

const (
	recordApproval        = "approval_recorded"
	recordOperation       = "operation_started"
	recordStepStart       = "step_started"
	recordStepFinish      = "step_finished"
	recordCancel          = "operation_cancelled"
	recordRollback        = "rollback_started"
	recordOperationFinish = "operation_finished"
)

type installationFile struct {
	SchemaVersion  int    `json:"schema_version"`
	InstallationID string `json:"installation_id"`
}

type headFile struct {
	SchemaVersion int    `json:"schema_version"`
	Sequence      uint64 `json:"sequence"`
	Digest        string `json:"digest"`
}

type operationRecord struct {
	ID         string `json:"id"`
	ApprovalID string `json:"approval_id"`
	PlanDigest string `json:"plan_digest"`
}

type stepFinishRecord struct {
	OperationID           string     `json:"operation_id"`
	StepID                string     `json:"step_id"`
	Attempt               uint32     `json:"attempt"`
	Status                StepStatus `json:"status"`
	ResultDigest          string     `json:"result_digest"`
	ReasonCode            string     `json:"reason_code"`
	FinishedAt            time.Time  `json:"finished_at"`
	ReconcilesInterrupted bool       `json:"reconciles_interrupted"`
}

type terminalRecord struct {
	OperationID string  `json:"operation_id"`
	Outcome     Outcome `json:"outcome"`
}

type journalRecord struct {
	SchemaVersion  int               `json:"schema_version"`
	Sequence       uint64            `json:"sequence"`
	PreviousDigest string            `json:"previous_digest"`
	InstallationID string            `json:"installation_id"`
	ObservedAt     time.Time         `json:"observed_at"`
	Kind           string            `json:"kind"`
	Approval       *Approval         `json:"approval,omitempty"`
	Operation      *operationRecord  `json:"operation,omitempty"`
	StepStart      *Step             `json:"step_start,omitempty"`
	StepFinish     *stepFinishRecord `json:"step_finish,omitempty"`
	OperationID    string            `json:"operation_id,omitempty"`
	Terminal       *terminalRecord   `json:"terminal,omitempty"`
}

// validateShape ensures a record has exactly the body named by Kind. This is
// checked both before append and during replay so ambiguous disk data fails.
func (record journalRecord) validateShape() error {
	if record.SchemaVersion != SchemaVersion || record.Sequence == 0 || !validID(record.InstallationID) || record.ObservedAt.IsZero() {
		return ErrCorrupt
	}
	if record.Sequence == 1 {
		if record.PreviousDigest != zeroDigest {
			return ErrCorrupt
		}
	} else if !validDigest(record.PreviousDigest) {
		return ErrCorrupt
	}
	bodies := 0
	if record.Approval != nil {
		bodies++
	}
	if record.Operation != nil {
		bodies++
	}
	if record.StepStart != nil {
		bodies++
	}
	if record.StepFinish != nil {
		bodies++
	}
	if record.OperationID != "" {
		bodies++
	}
	if record.Terminal != nil {
		bodies++
	}
	if bodies != 1 {
		return ErrCorrupt
	}
	switch record.Kind {
	case recordApproval:
		if record.Approval == nil {
			return ErrCorrupt
		}
	case recordOperation:
		if record.Operation == nil {
			return ErrCorrupt
		}
	case recordStepStart:
		if record.StepStart == nil {
			return ErrCorrupt
		}
	case recordStepFinish:
		if record.StepFinish == nil {
			return ErrCorrupt
		}
	case recordCancel, recordRollback:
		if !validID(record.OperationID) {
			return ErrCorrupt
		}
	case recordOperationFinish:
		if record.Terminal == nil {
			return ErrCorrupt
		}
	default:
		return ErrCorrupt
	}
	return nil
}
