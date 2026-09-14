package journal

import (
	"reflect"
	"time"
)

// applyRecord is the single replay state machine. Public mutations validate
// the same transition before append, then feed the durable record through it.
func (journal *Journal) applyRecord(record journalRecord) error {
	switch record.Kind {
	case recordApproval:
		if !record.Approval.IssuedAt.Equal(record.ObservedAt) {
			return ErrInvalidTransition
		}
		return journal.applyApproval(*record.Approval)
	case recordOperation:
		return journal.applyOperation(*record.Operation, record.ObservedAt)
	case recordStepStart:
		if !record.StepStart.StartedAt.Equal(record.ObservedAt) {
			return ErrInvalidTransition
		}
		return journal.applyStepStart(*record.StepStart)
	case recordStepFinish:
		if !record.StepFinish.FinishedAt.Equal(record.ObservedAt) {
			return ErrInvalidTransition
		}
		return journal.applyStepFinish(*record.StepFinish)
	case recordCancel:
		return journal.applyCancel(record.OperationID, record.ObservedAt)
	case recordRollback:
		return journal.applyRollback(record.OperationID)
	case recordOperationFinish:
		return journal.applyTerminal(*record.Terminal, record.ObservedAt)
	default:
		return ErrInvalidTransition
	}
}

// applyApproval makes approval IDs immutable while allowing exact replay.
func (journal *Journal) applyApproval(approval Approval) error {
	if err := validateStoredApproval(approval, journal.installationID); err != nil {
		return err
	}
	if existing, ok := journal.approvals[approval.ID]; ok {
		if reflect.DeepEqual(existing, approval) {
			return nil
		}
		return ErrConflict
	}
	journal.approvals[approval.ID] = cloneApproval(approval)
	return nil
}

// applyOperation verifies approval existence, digest equality, and expiry at
// the controller-observed start time before creating the in-memory projection.
func (journal *Journal) applyOperation(input operationRecord, observedAt time.Time) error {
	if !validID(input.ID) || !validID(input.ApprovalID) || !validDigest(input.PlanDigest) {
		return ErrInvalidInput
	}
	approval, ok := journal.approvals[input.ApprovalID]
	if !ok || approval.PlanDigest != input.PlanDigest || !observedAt.Before(approval.ExpiresAt) {
		return ErrInvalidTransition
	}
	if existing, ok := journal.operations[input.ID]; ok {
		if existing.ApprovalID == input.ApprovalID && existing.PlanDigest == input.PlanDigest {
			return nil
		}
		return ErrConflict
	}
	for _, existing := range journal.operations {
		if existing.ApprovalID == input.ApprovalID {
			return ErrConflict
		}
		if existing.FinishedAt.IsZero() {
			return ErrConflict
		}
	}
	journal.operations[input.ID] = &Operation{
		SchemaVersion: SchemaVersion, ID: input.ID, InstallationID: journal.installationID,
		ApprovalID: input.ApprovalID, PlanDigest: input.PlanDigest, StartedAt: observedAt,
		State: StateApproved, Steps: []Step{},
	}
	return nil
}

// applyStepStart handles both ordinary starts and the explicit record that
// abandons one recovered read-only attempt before starting its successor.
func (journal *Journal) applyStepStart(step Step) error {
	operation, ok := journal.operations[step.OperationID]
	if !ok {
		return ErrNotFound
	}
	if err := validateStoredStep(step); err != nil {
		return err
	}
	if existing := findStep(operation, step.StepID, step.Attempt); existing != nil {
		candidate := cloneStep(step)
		candidate.Recovered = existing.Recovered
		if reflect.DeepEqual(*existing, candidate) {
			return nil
		}
		return ErrConflict
	}
	if current := inFlight(operation); current != nil {
		if current.StepID != step.StepID || current.ComponentID != step.ComponentID || current.Phase != step.Phase || current.Mode != ModeReadOnly || !step.RetryOfInterrupted || step.Attempt != current.Attempt+1 {
			return ErrInvalidTransition
		}
		current.Status = StepFailed
		current.ReasonCode = "interrupted_retry"
		current.FinishedAt = step.StartedAt
		current.Recovered = false
	}
	if err := journal.validateStepTransition(operation, step); err != nil {
		return err
	}
	operation.Steps = append(operation.Steps, cloneStep(step))
	if step.Mode == ModeMutation {
		operation.MutationStarted = true
	}
	switch step.Phase {
	case PhasePreflight:
		operation.State = StatePreflighting
	case PhaseApply:
		operation.State = StateApplying
	case PhaseVerify:
		operation.State = StateVerifying
	case PhaseRollback:
		operation.State = StateRollingBack
	}
	return nil
}

// applyStepFinish resolves only the one matching in-flight attempt. A
// reconciliation marker preserves restart context in the durable record.
func (journal *Journal) applyStepFinish(result stepFinishRecord) error {
	operation, ok := journal.operations[result.OperationID]
	if !ok {
		return ErrNotFound
	}
	if !operation.FinishedAt.IsZero() {
		return ErrInvalidTransition
	}
	input := StepResultInput{OperationID: result.OperationID, StepID: result.StepID, Attempt: result.Attempt, Status: result.Status, ResultDigest: result.ResultDigest, ReasonCode: result.ReasonCode}
	if err := validateStepResultInput(input); err != nil || result.FinishedAt.IsZero() {
		return ErrInvalidInput
	}
	step := findStep(operation, result.StepID, result.Attempt)
	if step == nil {
		return ErrNotFound
	}
	if step.Status != StepRunning {
		if step.Status == result.Status && step.ResultDigest == result.ResultDigest && step.ReasonCode == result.ReasonCode && step.FinishedAt.Equal(result.FinishedAt) {
			return nil
		}
		return ErrConflict
	}
	if current := inFlight(operation); current != step {
		return ErrInvalidTransition
	}
	if result.ReconcilesInterrupted && step.Mode != ModeMutation {
		return ErrInvalidTransition
	}
	step.Status = result.Status
	step.ResultDigest = result.ResultDigest
	step.ReasonCode = result.ReasonCode
	step.FinishedAt = result.FinishedAt
	step.Recovered = false
	if result.Status == StepFailed {
		operation.Failure = failureFrom(*step)
		if step.Phase == PhaseRollback {
			operation.State = StateRecoveryRequired
		} else {
			operation.State = StateFailed
		}
		return nil
	}
	switch step.Phase {
	case PhasePreflight:
		operation.State = StatePreflighting
	case PhaseApply:
		operation.State = StateApplying
	case PhaseVerify:
		operation.State = StateVerifying
	case PhaseRollback:
		operation.State = StateRollingBack
	}
	return nil
}

// applyCancel enforces the irreversible boundary at the first mutation-start
// record rather than at a later result that may never be written.
func (journal *Journal) applyCancel(operationID string, observedAt time.Time) error {
	operation, ok := journal.operations[operationID]
	if !ok {
		return ErrNotFound
	}
	if operation.State == StateCancelled {
		return nil
	}
	if operation.MutationStarted || inFlight(operation) != nil || (operation.State != StateApproved && operation.State != StatePreflighting) {
		return ErrInvalidTransition
	}
	operation.State = StateCancelled
	operation.FinishedAt = observedAt
	return nil
}

// applyRollback requires actual rollback-eligible mutation history. It may be
// operator-initiated after successful mutation; rollback is not conditional on
// manufacturing a later failure record.
func (journal *Journal) applyRollback(operationID string) error {
	operation, ok := journal.operations[operationID]
	if !ok {
		return ErrNotFound
	}
	if operation.RollbackStarted {
		return nil
	}
	rollbackState := operation.State == StateApplying || operation.State == StateVerifying || operation.State == StateFailed
	if !rollbackState || !operation.MutationStarted || inFlight(operation) != nil || !operation.FinishedAt.IsZero() || !hasRollbackCandidate(operation) {
		return ErrInvalidTransition
	}
	operation.RollbackStarted = true
	operation.State = StateRollingBack
	return nil
}

// applyTerminal validates outcome-specific completeness before sealing an
// operation. Recovery-required uniquely permits an unresolved mutation step.
func (journal *Journal) applyTerminal(terminal terminalRecord, observedAt time.Time) error {
	operation, ok := journal.operations[terminal.OperationID]
	if !ok {
		return ErrNotFound
	}
	wanted := terminalState(terminal.Outcome)
	if operation.State == wanted && !operation.FinishedAt.IsZero() {
		return nil
	}
	if inFlight(operation) != nil && terminal.Outcome != OutcomeRecoveryRequired {
		return ErrInvalidTransition
	}
	switch terminal.Outcome {
	case OutcomeComplete:
		if !canComplete(operation, journal.approvals[operation.ApprovalID]) {
			return ErrInvalidTransition
		}
	case OutcomeFailed:
		if operation.State != StateFailed || operation.MutationStarted {
			return ErrInvalidTransition
		}
	case OutcomeRecoveryRequired:
		step := inFlight(operation)
		interruptedMutation := step != nil && step.Mode == ModeMutation && (operation.State == StateApplying || operation.State == StateRollingBack || operation.State == StateRecoveryRequired)
		possiblyLiveMutation := operation.MutationStarted && operation.FinishedAt.IsZero()
		if operation.State != StateRecoveryRequired && !interruptedMutation && !possiblyLiveMutation {
			return ErrInvalidTransition
		}
	case OutcomeRolledBack:
		if !canReportRolledBack(operation) {
			return ErrInvalidTransition
		}
	default:
		return ErrInvalidInput
	}
	operation.State = wanted
	operation.FinishedAt = observedAt
	return nil
}

func terminalState(outcome Outcome) OperationState {
	switch outcome {
	case OutcomeComplete:
		return StateComplete
	case OutcomeFailed:
		return StateFailed
	case OutcomeRecoveryRequired:
		return StateRecoveryRequired
	case OutcomeRolledBack:
		return StateRolledBack
	default:
		return ""
	}
}

// validateStoredApproval treats replayed content as hostile even after its
// frame checksum passes; checksums detect change but do not prove semantics.
func validateStoredApproval(approval Approval, installationID string) error {
	if approval.SchemaVersion != SchemaVersion || approval.InstallationID != installationID || !validID(approval.ID) || !validID(approval.ActorID) || !validDigest(approval.PlanDigest) || !validDigest(approval.ManifestDigest) || !validDigest(approval.AuthorityDigest) || approval.IssuedAt.IsZero() || !approval.IssuedAt.Before(approval.ExpiresAt) || approval.Components == nil || len(approval.Components) == 0 || len(approval.Components) > 64 || approval.SecretRevisions == nil {
		return ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(approval.Components))
	for index, component := range approval.Components {
		if component.Index != index+1 || !validID(component.ComponentID) || len(component.Adapter) > 160 || !adapterPattern.MatchString(component.Adapter) || !validDigest(component.ArtifactDigest) || !validDigest(component.ConfigurationDigest) || !sortedUnique(component.Capabilities) {
			return ErrInvalidInput
		}
		if _, exists := seen[component.ComponentID]; exists {
			return ErrInvalidInput
		}
		seen[component.ComponentID] = struct{}{}
		for _, capability := range component.Capabilities {
			if len(capability) > 160 || !capabilityPattern.MatchString(capability) {
				return ErrInvalidInput
			}
		}
	}
	for index, secret := range approval.SecretRevisions {
		if !validID(secret.ComponentID) || !validID(secret.Slot) || !validDigest(secret.RevisionDigest) {
			return ErrInvalidInput
		}
		if index > 0 && secretKey(approval.SecretRevisions[index-1]) >= secretKey(secret) {
			return ErrInvalidInput
		}
		if _, ok := seen[secret.ComponentID]; !ok {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateStoredStep(step Step) error {
	input := StepInput{OperationID: step.OperationID, StepID: step.StepID, ComponentID: step.ComponentID, Phase: step.Phase, Mode: step.Mode, Attempt: step.Attempt, IdempotencyDigest: step.IdempotencyDigest, Rollback: step.Rollback, CompensatesStepID: step.CompensatesStepID}
	if err := validateStepInput(input); err != nil || step.StartedAt.IsZero() || !step.FinishedAt.IsZero() || step.Status != StepRunning || step.ResultDigest != "" || step.ReasonCode != "" || step.Recovered {
		return ErrInvalidInput
	}
	if step.RetryOfInterrupted && step.Attempt < 2 {
		return ErrInvalidInput
	}
	return nil
}

// validateStepTransition enforces monotonic phases, component membership,
// mutation-digest uniqueness, and exact rollback compensation identity.
func (journal *Journal) validateStepTransition(operation *Operation, step Step) error {
	if isTerminal(operation.State) || !operation.FinishedAt.IsZero() || !journal.approvalHasComponent(operation.ApprovalID, step.ComponentID) {
		return ErrInvalidTransition
	}
	if prior := latestStepByID(operation, step.StepID); prior != nil {
		if !step.RetryOfInterrupted || step.Attempt != prior.Attempt+1 || prior.Mode != ModeReadOnly || prior.ReasonCode != "interrupted_retry" {
			return ErrConflict
		}
	} else if step.Attempt != 1 || step.RetryOfInterrupted {
		return ErrInvalidTransition
	}
	if step.Mode == ModeMutation {
		for _, prior := range operation.Steps {
			if prior.Mode == ModeMutation && prior.IdempotencyDigest == step.IdempotencyDigest {
				return ErrConflict
			}
		}
	}
	switch step.Phase {
	case PhasePreflight:
		if operation.MutationStarted || operation.RollbackStarted || operation.Failure != nil || (operation.State != StateApproved && operation.State != StatePreflighting && operation.State != StateRetryRequired) {
			return ErrInvalidTransition
		}
	case PhaseApply:
		if operation.RollbackStarted || operation.Failure != nil || (operation.State != StatePreflighting && operation.State != StateApplying) || !journal.allComponentsSucceededPhase(operation, PhasePreflight) {
			return ErrInvalidTransition
		}
	case PhaseVerify:
		if operation.RollbackStarted || operation.Failure != nil || (operation.State != StatePreflighting && operation.State != StateApplying && operation.State != StateVerifying && operation.State != StateRetryRequired) || !journal.allComponentsSucceededPhase(operation, PhasePreflight) {
			return ErrInvalidTransition
		}
	case PhaseRollback:
		if !operation.RollbackStarted || operation.State != StateRollingBack {
			return ErrInvalidTransition
		}
		original := latestRollbackEligibleMutation(operation, step.CompensatesStepID)
		if original == nil || original.ComponentID != step.ComponentID || original.Rollback.ReferenceDigest != step.Rollback.ReferenceDigest || compensated(operation, original.StepID) {
			return ErrInvalidTransition
		}
	}
	return nil
}

func isTerminal(state OperationState) bool {
	return state == StateComplete || state == StateRolledBack || state == StateCancelled
}

func (journal *Journal) approvalHasComponent(approvalID, componentID string) bool {
	approval, ok := journal.approvals[approvalID]
	if !ok {
		return false
	}
	for _, component := range approval.Components {
		if component.ComponentID == componentID {
			return true
		}
	}
	return false
}

func failureFrom(step Step) *FailurePoint {
	return &FailurePoint{Phase: step.Phase, ComponentID: step.ComponentID, StepID: step.StepID, Attempt: step.Attempt, ReasonCode: step.ReasonCode}
}

func inFlight(operation *Operation) *Step {
	for index := len(operation.Steps) - 1; index >= 0; index-- {
		if operation.Steps[index].Status == StepRunning {
			return &operation.Steps[index]
		}
	}
	return nil
}

func findStep(operation *Operation, id string, attempt uint32) *Step {
	for index := range operation.Steps {
		if operation.Steps[index].StepID == id && operation.Steps[index].Attempt == attempt {
			return &operation.Steps[index]
		}
	}
	return nil
}

func latestStepByID(operation *Operation, id string) *Step {
	for index := len(operation.Steps) - 1; index >= 0; index-- {
		if operation.Steps[index].StepID == id {
			return &operation.Steps[index]
		}
	}
	return nil
}

func (journal *Journal) allComponentsSucceededPhase(operation *Operation, phase Phase) bool {
	approval := journal.approvals[operation.ApprovalID]
	succeeded := make(map[string]struct{})
	for _, step := range operation.Steps {
		if step.Phase == phase && step.Status == StepSucceeded {
			succeeded[step.ComponentID] = struct{}{}
		}
	}
	if len(succeeded) != len(approval.Components) {
		return false
	}
	for _, component := range approval.Components {
		if _, ok := succeeded[component.ComponentID]; !ok {
			return false
		}
	}
	return true
}

func latestRollbackEligibleMutation(operation *Operation, id string) *Step {
	for index := len(operation.Steps) - 1; index >= 0; index-- {
		step := &operation.Steps[index]
		if step.StepID == id && step.Mode == ModeMutation && step.Phase == PhaseApply && (step.Status == StepSucceeded || step.Status == StepFailed) {
			return step
		}
	}
	return nil
}

func compensated(operation *Operation, stepID string) bool {
	for _, step := range operation.Steps {
		if step.Phase == PhaseRollback && step.CompensatesStepID == stepID && step.Status == StepSucceeded {
			return true
		}
	}
	return false
}

func hasRollbackCandidate(operation *Operation) bool {
	for _, step := range operation.Steps {
		if step.Phase == PhaseApply && step.Mode == ModeMutation && step.Status != StepRunning && step.Rollback.ReferenceDigest != "" && !compensated(operation, step.StepID) {
			return true
		}
	}
	return false
}

// canComplete requires a fresh successful verification for every approved
// component and no failed or unresolved mutation.
func canComplete(operation *Operation, approval Approval) bool {
	if operation.Failure != nil || operation.RollbackStarted || inFlight(operation) != nil {
		return false
	}
	verified := make(map[string]struct{})
	for _, step := range operation.Steps {
		if step.Phase == PhaseVerify && step.Status == StepSucceeded {
			verified[step.ComponentID] = struct{}{}
		}
		if step.Mode == ModeMutation && step.Status != StepSucceeded {
			return false
		}
	}
	if len(verified) != len(approval.Components) {
		return false
	}
	for _, component := range approval.Components {
		if _, ok := verified[component.ComponentID]; !ok {
			return false
		}
	}
	return true
}

// canReportRolledBack rejects recovery-only work and requires compensation for
// every finished mutation because a failed result can still be partially live.
func canReportRolledBack(operation *Operation) bool {
	if !operation.RollbackStarted || operation.State != StateRollingBack || inFlight(operation) != nil {
		return false
	}
	for _, step := range operation.Steps {
		if step.Phase == PhaseApply && step.Mode == ModeMutation && step.Rollback.RecoveryOnlyReason != "" {
			return false
		}
		if step.Phase == PhaseRollback && step.Status == StepFailed {
			return false
		}
		if step.Phase == PhaseApply && step.Mode == ModeMutation && step.Status != StepRunning {
			if step.Rollback.RecoveryOnlyReason != "" || !compensated(operation, step.StepID) {
				return false
			}
		}
	}
	return true
}

func cloneApproval(value Approval) Approval {
	copy := value
	copy.Components = append([]ApprovedComponent(nil), value.Components...)
	for index := range copy.Components {
		copy.Components[index].Capabilities = append([]string(nil), value.Components[index].Capabilities...)
	}
	copy.SecretRevisions = append([]SecretRevision{}, value.SecretRevisions...)
	return copy
}

func cloneStep(value Step) Step { return value }

func cloneOperation(value Operation) Operation {
	copy := value
	copy.Steps = append([]Step(nil), value.Steps...)
	if value.Failure != nil {
		failure := *value.Failure
		copy.Failure = &failure
	}
	return copy
}
