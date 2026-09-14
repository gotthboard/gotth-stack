package journal

import "reflect"

// StartOperation binds one operation ID to one unexpired approval and exact
// plan digest. It records no authority beyond that already in the approval.
func (journal *Journal) StartOperation(input OperationInput) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if err := validateOperationInput(input); err != nil {
		return Operation{}, err
	}
	if existing, ok := journal.operations[input.ID]; ok {
		if existing.ApprovalID == input.ApprovalID && existing.PlanDigest == input.PlanDigest {
			return cloneOperation(*existing), nil
		}
		return Operation{}, ErrConflict
	}
	approval, ok := journal.approvals[input.ApprovalID]
	if !ok {
		return Operation{}, ErrNotFound
	}
	now := observedTime(journal.now())
	if !now.Before(approval.ExpiresAt) {
		return Operation{}, ErrExpired
	}
	if approval.PlanDigest != input.PlanDigest {
		return Operation{}, ErrConflict
	}
	body := operationRecord{ID: input.ID, ApprovalID: input.ApprovalID, PlanDigest: input.PlanDigest}
	record := journalRecord{Kind: recordOperation, ObservedAt: now, Operation: &body}
	return journal.appendOperationRecord(record, input.ID)
}

// BeginStep records intent before a caller may perform work. Mutation intent
// is rejected unless it carries an idempotency digest and explicit recovery
// policy; the package itself never performs the work.
func (journal *Journal) BeginStep(input StepInput) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if err := validateStepInput(input); err != nil {
		return Operation{}, err
	}
	operation, ok := journal.operations[input.OperationID]
	if !ok {
		return Operation{}, ErrNotFound
	}
	candidate := Step{
		OperationID: input.OperationID, StepID: input.StepID, ComponentID: input.ComponentID,
		Phase: input.Phase, Mode: input.Mode, Attempt: input.Attempt,
		IdempotencyDigest: input.IdempotencyDigest, Rollback: input.Rollback,
		CompensatesStepID: input.CompensatesStepID, StartedAt: observedTime(journal.now()), Status: StepRunning,
	}
	if current := inFlight(operation); current != nil && current.Recovered && current.Mode == ModeReadOnly && current.StepID == input.StepID && current.ComponentID == input.ComponentID && current.Phase == input.Phase && input.Attempt == current.Attempt+1 {
		candidate.RetryOfInterrupted = true
	}
	if existing := findStep(operation, input.StepID, input.Attempt); existing != nil {
		copy := candidate
		copy.StartedAt = existing.StartedAt
		copy.Recovered = existing.Recovered
		if stepRequestEqual(*existing, copy) {
			return cloneOperation(*operation), nil
		}
		return Operation{}, ErrConflict
	}
	record := journalRecord{Kind: recordStepStart, ObservedAt: candidate.StartedAt, StepStart: &candidate}
	return journal.appendOperationRecord(record, input.OperationID)
}

// FinishStep durably records either a digest result or a bounded failure code.
// Exact duplicate completion is a read; conflicting reuse is rejected.
func (journal *Journal) FinishStep(input StepResultInput) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if err := validateStepResultInput(input); err != nil {
		return Operation{}, err
	}
	operation, ok := journal.operations[input.OperationID]
	if !ok {
		return Operation{}, ErrNotFound
	}
	step := findStep(operation, input.StepID, input.Attempt)
	if step == nil {
		return Operation{}, ErrNotFound
	}
	if step.Status != StepRunning {
		if step.Status == input.Status && step.ResultDigest == input.ResultDigest && step.ReasonCode == input.ReasonCode {
			return cloneOperation(*operation), nil
		}
		return Operation{}, ErrConflict
	}
	if step.Recovered && step.Mode == ModeReadOnly {
		return Operation{}, ErrInvalidTransition
	}
	finish := stepFinishRecord{
		OperationID: input.OperationID, StepID: input.StepID, Attempt: input.Attempt,
		Status: input.Status, ResultDigest: input.ResultDigest, ReasonCode: input.ReasonCode,
		FinishedAt:            observedTime(journal.now()),
		ReconcilesInterrupted: step.Recovered && step.Mode == ModeMutation,
	}
	record := journalRecord{Kind: recordStepFinish, ObservedAt: finish.FinishedAt, StepFinish: &finish}
	return journal.appendOperationRecord(record, input.OperationID)
}

func (journal *Journal) Cancel(operationID string) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if !validID(operationID) {
		return Operation{}, ErrInvalidInput
	}
	if operation, ok := journal.operations[operationID]; ok && operation.State == StateCancelled {
		return cloneOperation(*operation), nil
	}
	record := journalRecord{Kind: recordCancel, ObservedAt: observedTime(journal.now()), OperationID: operationID}
	return journal.appendOperationRecord(record, operationID)
}

func (journal *Journal) BeginRollback(operationID string) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if !validID(operationID) {
		return Operation{}, ErrInvalidInput
	}
	if operation, ok := journal.operations[operationID]; ok && operation.RollbackStarted {
		return cloneOperation(*operation), nil
	}
	record := journalRecord{Kind: recordRollback, ObservedAt: observedTime(journal.now()), OperationID: operationID}
	return journal.appendOperationRecord(record, operationID)
}

func (journal *Journal) FinishOperation(operationID string, outcome Outcome) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if !validID(operationID) || terminalState(outcome) == "" {
		return Operation{}, ErrInvalidInput
	}
	if operation, ok := journal.operations[operationID]; ok && operation.State == terminalState(outcome) && !operation.FinishedAt.IsZero() {
		return cloneOperation(*operation), nil
	}
	body := terminalRecord{OperationID: operationID, Outcome: outcome}
	record := journalRecord{Kind: recordOperationFinish, ObservedAt: observedTime(journal.now()), Terminal: &body}
	return journal.appendOperationRecord(record, operationID)
}

func (journal *Journal) Operation(operationID string) (Operation, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, err
	}
	if !validID(operationID) {
		return Operation{}, ErrInvalidInput
	}
	operation, ok := journal.operations[operationID]
	if !ok {
		return Operation{}, ErrNotFound
	}
	return cloneOperation(*operation), nil
}

// ActiveOperation returns the sole unfinished operation, if any, so restart
// recovery does not depend on a second authority database.
func (journal *Journal) ActiveOperation() (Operation, bool, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.usable(); err != nil {
		return Operation{}, false, err
	}
	for _, operation := range journal.operations {
		if operation.FinishedAt.IsZero() {
			return cloneOperation(*operation), true, nil
		}
	}
	return Operation{}, false, nil
}

// appendOperationRecord previews against a deep copy so no durable record can
// encode an invalid transition, then appends and applies the exact same event.
func (journal *Journal) appendOperationRecord(record journalRecord, operationID string) (Operation, error) {
	if err := journal.preview(record); err != nil {
		return Operation{}, err
	}
	persisted, err := journal.appendRecord(record)
	if err != nil {
		return Operation{}, err
	}
	if err := journal.applyRecord(persisted); err != nil {
		journal.poisoned = true
		return Operation{}, ErrRecoveryRequired
	}
	operation, ok := journal.operations[operationID]
	if !ok {
		journal.poisoned = true
		return Operation{}, ErrRecoveryRequired
	}
	return cloneOperation(*operation), nil
}

func (journal *Journal) usable() error {
	if journal.closed {
		return ErrClosed
	}
	if journal.poisoned {
		return ErrRecoveryRequired
	}
	return nil
}

func (journal *Journal) preview(record journalRecord) error {
	copy := &Journal{installationID: journal.installationID, approvals: make(map[string]Approval, len(journal.approvals)), operations: make(map[string]*Operation, len(journal.operations))}
	for id, approval := range journal.approvals {
		copy.approvals[id] = cloneApproval(approval)
	}
	for id, operation := range journal.operations {
		cloned := cloneOperation(*operation)
		copy.operations[id] = &cloned
	}
	record = recordWithEnvelope(record, journal.head.Sequence+1, journal.head.Digest)
	record.InstallationID = journal.installationID
	if err := record.validateShape(); err != nil {
		return ErrInvalidInput
	}
	return copy.applyRecord(record)
}

func recordWithEnvelope(record journalRecord, sequence uint64, previousDigest string) journalRecord {
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SchemaVersion
	}
	if record.Sequence == 0 {
		record.Sequence = sequence
	}
	if record.PreviousDigest == "" {
		record.PreviousDigest = previousDigest
	}
	return record
}

func stepRequestEqual(existing, candidate Step) bool {
	existing.FinishedAt = candidate.FinishedAt
	existing.Status = candidate.Status
	existing.ResultDigest = candidate.ResultDigest
	existing.ReasonCode = candidate.ReasonCode
	existing.Recovered = candidate.Recovered
	existing.RetryOfInterrupted = candidate.RetryOfInterrupted
	return reflect.DeepEqual(existing, candidate)
}
