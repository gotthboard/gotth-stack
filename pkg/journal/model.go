// Package journal records durable GOTTH Stack approvals and operation state.
// It deliberately performs no deployment or adapter work.
package journal

import (
	"errors"
	"time"
)

const (
	SchemaVersion  = 1
	MaxPayloadSize = 4 << 20
	MaxLogSize     = 512 << 20
	MaxRecords     = 262144
)

var (
	ErrInvalidInput      = errors.New("journal input is invalid")
	ErrCorrupt           = errors.New("journal state is corrupt")
	ErrLocked            = errors.New("journal is locked")
	ErrClosed            = errors.New("journal is closed")
	ErrNotFound          = errors.New("journal record was not found")
	ErrConflict          = errors.New("journal idempotency conflict")
	ErrExpired           = errors.New("journal approval is expired")
	ErrInvalidTransition = errors.New("journal transition is invalid")
	ErrRecoveryRequired  = errors.New("journal recovery is required")
	ErrLimit             = errors.New("journal storage limit exceeded")
	ErrStorage           = errors.New("journal storage operation failed")
)

type Phase string

const (
	PhasePreflight Phase = "preflight"
	PhaseApply     Phase = "apply"
	PhaseVerify    Phase = "verify"
	PhaseRollback  Phase = "rollback"
)

type StepMode string

const (
	ModeReadOnly StepMode = "read_only"
	ModeMutation StepMode = "mutation"
)

type StepStatus string

const (
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
)

type OperationState string

const (
	StateApproved         OperationState = "approved"
	StatePreflighting     OperationState = "preflighting"
	StateApplying         OperationState = "applying"
	StateVerifying        OperationState = "verifying"
	StateFailed           OperationState = "failed"
	StateRetryRequired    OperationState = "retry_required"
	StateRollingBack      OperationState = "rolling_back"
	StateRecoveryRequired OperationState = "recovery_required"
	StateComplete         OperationState = "complete"
	StateRolledBack       OperationState = "rolled_back"
	StateCancelled        OperationState = "cancelled"
)

type Outcome string

const (
	OutcomeComplete         Outcome = "complete"
	OutcomeFailed           Outcome = "failed"
	OutcomeRecoveryRequired Outcome = "recovery_required"
	OutcomeRolledBack       Outcome = "rolled_back"
)

type RecoveryOnlyReason string

const (
	RecoveryNoRollback RecoveryOnlyReason = "no_rollback_available"
	RecoveryExternal   RecoveryOnlyReason = "external_reconciliation_required"
)

type SecretRevision struct {
	ComponentID    string `json:"component_id"`
	Slot           string `json:"slot"`
	RevisionDigest string `json:"revision_digest"`
}

type ApprovalInput struct {
	ID              string           `json:"id"`
	ActorID         string           `json:"actor_id"`
	AuthorityDigest string           `json:"authority_digest"`
	ExpiresAt       time.Time        `json:"expires_at"`
	SecretRevisions []SecretRevision `json:"secret_revisions"`
}

type ApprovedComponent struct {
	Index               int      `json:"index"`
	ComponentID         string   `json:"component_id"`
	Adapter             string   `json:"adapter"`
	ArtifactDigest      string   `json:"artifact_digest"`
	ConfigurationDigest string   `json:"configuration_digest"`
	Capabilities        []string `json:"capabilities"`
}

type Approval struct {
	SchemaVersion   int                 `json:"schema_version"`
	ID              string              `json:"id"`
	InstallationID  string              `json:"installation_id"`
	PlanDigest      string              `json:"plan_digest"`
	ManifestDigest  string              `json:"manifest_digest"`
	ActorID         string              `json:"actor_id"`
	AuthorityDigest string              `json:"authority_digest"`
	IssuedAt        time.Time           `json:"issued_at"`
	ExpiresAt       time.Time           `json:"expires_at"`
	Components      []ApprovedComponent `json:"components"`
	SecretRevisions []SecretRevision    `json:"secret_revisions"`
}

type OperationInput struct {
	ID         string `json:"id"`
	ApprovalID string `json:"approval_id"`
	PlanDigest string `json:"plan_digest"`
}

type RollbackPolicy struct {
	ReferenceDigest    string             `json:"reference_digest"`
	RecoveryOnlyReason RecoveryOnlyReason `json:"recovery_only_reason"`
}

type StepInput struct {
	OperationID       string         `json:"operation_id"`
	StepID            string         `json:"step_id"`
	ComponentID       string         `json:"component_id"`
	Phase             Phase          `json:"phase"`
	Mode              StepMode       `json:"mode"`
	Attempt           uint32         `json:"attempt"`
	IdempotencyDigest string         `json:"idempotency_digest"`
	Rollback          RollbackPolicy `json:"rollback"`
	CompensatesStepID string         `json:"compensates_step_id"`
}

type StepResultInput struct {
	OperationID  string     `json:"operation_id"`
	StepID       string     `json:"step_id"`
	Attempt      uint32     `json:"attempt"`
	Status       StepStatus `json:"status"`
	ResultDigest string     `json:"result_digest"`
	ReasonCode   string     `json:"reason_code"`
}

type Step struct {
	OperationID        string         `json:"operation_id"`
	StepID             string         `json:"step_id"`
	ComponentID        string         `json:"component_id"`
	Phase              Phase          `json:"phase"`
	Mode               StepMode       `json:"mode"`
	Attempt            uint32         `json:"attempt"`
	IdempotencyDigest  string         `json:"idempotency_digest"`
	Rollback           RollbackPolicy `json:"rollback"`
	CompensatesStepID  string         `json:"compensates_step_id"`
	StartedAt          time.Time      `json:"started_at"`
	FinishedAt         time.Time      `json:"finished_at"`
	Status             StepStatus     `json:"status"`
	ResultDigest       string         `json:"result_digest"`
	ReasonCode         string         `json:"reason_code"`
	Recovered          bool           `json:"recovered"`
	RetryOfInterrupted bool           `json:"retry_of_interrupted"`
}

type FailurePoint struct {
	Phase       Phase  `json:"phase"`
	ComponentID string `json:"component_id"`
	StepID      string `json:"step_id"`
	Attempt     uint32 `json:"attempt"`
	ReasonCode  string `json:"reason_code"`
}

type Operation struct {
	SchemaVersion   int            `json:"schema_version"`
	ID              string         `json:"id"`
	InstallationID  string         `json:"installation_id"`
	ApprovalID      string         `json:"approval_id"`
	PlanDigest      string         `json:"plan_digest"`
	StartedAt       time.Time      `json:"started_at"`
	FinishedAt      time.Time      `json:"finished_at"`
	State           OperationState `json:"state"`
	RollbackStarted bool           `json:"rollback_started"`
	MutationStarted bool           `json:"mutation_started"`
	Failure         *FailurePoint  `json:"failure"`
	Steps           []Step         `json:"steps"`
}

type Recovery struct {
	TruncatedBytes int64  `json:"truncated_bytes"`
	HeadRepaired   bool   `json:"head_repaired"`
	RecordCount    uint64 `json:"record_count"`
}
