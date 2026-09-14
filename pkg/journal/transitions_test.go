package journal

import (
	"errors"
	"testing"
)

func TestTransitionFailuresAreDurableAndBounded(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-early", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("early apply error=%v", err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "missing", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing finish error=%v", err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "wrong-component", ComponentID: "other", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("component error=%v", err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "preflight-database", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "parallel", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("parallel step error=%v", err)
	}
	operation, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "preflight-database", Attempt: 1, Status: StepFailed, ReasonCode: "probe_failed"})
	if err != nil || operation.State != StateFailed || operation.Failure == nil || operation.Failure.ReasonCode != "probe_failed" {
		t.Fatalf("failed operation=%#v err=%v", operation, err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "verify-after-failure", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("verify after failure error=%v", err)
	}
	operation, err = journal.FinishOperation(operation.ID, OutcomeFailed)
	if err != nil || operation.State != StateFailed || operation.FinishedAt.IsZero() {
		t.Fatalf("terminal failure=%#v err=%v", operation, err)
	}
	if _, err := journal.BeginRollback(operation.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("rollback after terminal failure error=%v", err)
	}
}

func TestDuplicateAndConflictingRequests(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	if _, err := journal.StartOperation(OperationInput{ID: operation.ID, ApprovalID: "approval-a", PlanDigest: plan.Digest}); err != nil {
		t.Fatalf("duplicate operation: %v", err)
	}
	if _, err := journal.StartOperation(OperationInput{ID: operation.ID, ApprovalID: "other", PlanDigest: plan.Digest}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting operation error=%v", err)
	}
	if _, err := journal.StartOperation(OperationInput{ID: "operation-b", ApprovalID: "approval-a", PlanDigest: plan.Digest}); !errors.Is(err, ErrConflict) {
		t.Fatalf("approval replay error=%v", err)
	}
	input := StepInput{OperationID: operation.ID, StepID: "preflight-database", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1}
	if _, err := journal.BeginStep(input); err != nil {
		t.Fatal(err)
	}
	count := journal.recordCount
	if _, err := journal.BeginStep(input); err != nil || journal.recordCount != count {
		t.Fatalf("duplicate begin err=%v count=%d", err, journal.recordCount)
	}
	conflict := input
	conflict.ComponentID = "other"
	if _, err := journal.BeginStep(conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting begin error=%v", err)
	}
	result := StepResultInput{OperationID: operation.ID, StepID: input.StepID, Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}
	if _, err := journal.FinishStep(result); err != nil {
		t.Fatal(err)
	}
	count = journal.recordCount
	if _, err := journal.FinishStep(result); err != nil || journal.recordCount != count {
		t.Fatalf("duplicate finish err=%v count=%d", err, journal.recordCount)
	}
	result.ResultDigest = digestB
	if _, err := journal.FinishStep(result); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting finish error=%v", err)
	}
}

func TestRecoveryTerminalAndRecoveryOnlyRollbackRefusal(t *testing.T) {
	t.Run("terminal-recovery", func(t *testing.T) {
		journal, root, now := newTestJournal(t)
		plan := testPlan(t, false)
		recordTestApproval(t, journal, plan, now)
		operation := startTestOperation(t, journal, plan)
		finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
		if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
			t.Fatal(err)
		}
		_ = journal.Close()
		reopened, _, err := Open(root, "installation-a")
		if err != nil {
			t.Fatal(err)
		}
		operation, err = reopened.FinishOperation(operation.ID, OutcomeRecoveryRequired)
		if err != nil || operation.State != StateRecoveryRequired || operation.FinishedAt.IsZero() {
			t.Fatalf("terminal recovery=%#v err=%v", operation, err)
		}
		_ = reopened.Close()
		replayed, _, err := Open(root, "installation-a")
		if err != nil {
			t.Fatalf("terminal recovery replay: %v", err)
		}
		operation, _ = replayed.Operation(operation.ID)
		if operation.State != StateRecoveryRequired || operation.FinishedAt.IsZero() {
			t.Fatalf("replayed=%#v", operation)
		}
		_ = replayed.Close()
	})

	t.Run("recovery-only", func(t *testing.T) {
		journal, _, now := newTestJournal(t)
		plan := testPlan(t, false)
		recordTestApproval(t, journal, plan, now)
		operation := startTestOperation(t, journal, plan)
		finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
		if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{RecoveryOnlyReason: RecoveryExternal}}); err != nil {
			t.Fatal(err)
		}
		operation, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepFailed, ReasonCode: "external_failure"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = journal.BeginRollback(operation.ID); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("recovery-only rollback error=%v operation=%#v", err, operation)
		}
		if _, err = journal.FinishOperation(operation.ID, OutcomeFailed); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("mutation hidden as ordinary failure: %v", err)
		}
		operation, err = journal.FinishOperation(operation.ID, OutcomeRecoveryRequired)
		if err != nil || operation.State != StateRecoveryRequired || operation.FinishedAt.IsZero() {
			t.Fatalf("recovery-only terminal=%#v err=%v", operation, err)
		}
	})
}

func TestVerifyFailureAfterMutationRequiresRollbackOrRecovery(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "verify-database", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "verify-database", Attempt: 1, Status: StepFailed, ReasonCode: "verify_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = journal.FinishOperation(operation.ID, OutcomeFailed); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("mutation hidden as ordinary failure: %v", err)
	}
	operation, err = journal.FinishOperation(operation.ID, OutcomeRecoveryRequired)
	if err != nil || operation.State != StateRecoveryRequired {
		t.Fatalf("recovery terminal=%#v err=%v", operation, err)
	}
}

func TestSuccessfulMutationMayBeRolledBackWithoutInventingFailure(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.BeginRollback(operation.ID)
	if err != nil || operation.State != StateRollingBack {
		t.Fatalf("operator rollback=%#v err=%v", operation, err)
	}
}

func TestSuccessfulMutationMayTerminateRecoveryRequired(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.FinishOperation(operation.ID, OutcomeRecoveryRequired)
	if err != nil || operation.State != StateRecoveryRequired || operation.FinishedAt.IsZero() {
		t.Fatalf("explicit recovery=%#v err=%v", operation, err)
	}
}

func TestInterruptedRollbackCanResumeAfterReconciliation(t *testing.T) {
	journal, root, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "verify-database", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "verify-database", Attempt: 1, Status: StepFailed, ReasonCode: "verify_failed"}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.BeginRollback(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "rollback-database", ComponentID: "database", Phase: PhaseRollback, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestC, Rollback: RollbackPolicy{ReferenceDigest: digestB}, CompensatesStepID: "apply-database"}); err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	reopened, _, err := Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	operation, err = reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "rollback-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestC})
	if err != nil || operation.State != StateRollingBack {
		t.Fatalf("reconcile rollback=%#v err=%v", operation, err)
	}
	_ = reopened.Close()
	reopened, _, err = Open(root, "installation-a")
	if err != nil {
		t.Fatalf("reconciled rollback replay: %v", err)
	}
	operation, _ = reopened.Operation(operation.ID)
	if operation.State != StateRollingBack {
		t.Fatalf("replayed rollback=%#v", operation)
	}
	operation, err = reopened.FinishOperation(operation.ID, OutcomeRolledBack)
	if err != nil || operation.State != StateRolledBack {
		t.Fatalf("terminal=%#v err=%v", operation, err)
	}
}

func TestPhaseOrderPreventsStaleVerification(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	finishReadOnly(t, journal, operation.ID, "verify-database", "database", PhaseVerify, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "late-apply", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("late apply error=%v", err)
	}
}

func TestMutationIdempotencyDigestCannotMoveBetweenSteps(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	first := StepInput{OperationID: operation.ID, StepID: "apply-one", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}
	if _, err := journal.BeginStep(first); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: first.StepID, Attempt: 1, Status: StepSucceeded, ResultDigest: digestC}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.StepID = "apply-two"
	if _, err := journal.BeginStep(second); !errors.Is(err, ErrConflict) {
		t.Fatalf("digest reuse=%v", err)
	}
}

func TestFailedMutationStillRequiresCompensation(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepFailed, ReasonCode: "partial_failure"})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = journal.BeginRollback(operation.ID)
	if err != nil {
		t.Fatalf("begin rollback=%v", err)
	}
	if _, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "rollback-database", ComponentID: "database", Phase: PhaseRollback, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestC, Rollback: RollbackPolicy{ReferenceDigest: digestB}, CompensatesStepID: "apply-database"}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "rollback-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestC}); err != nil {
		t.Fatal(err)
	}
	operation, err = journal.FinishOperation(operation.ID, OutcomeRolledBack)
	if err != nil || operation.State != StateRolledBack {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestPublicAPIRejectsMissingInvalidAndIncompleteRequests(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	badPlan := plan
	badPlan.Digest = digestC
	if _, err := journal.RecordApproval(badPlan, ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad plan=%v", err)
	}
	secretPlan := testPlan(t, true)
	if _, err := journal.RecordApproval(secretPlan, ApprovalInput{ID: "approval-secret", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing secret=%v", err)
	}
	recordTestApproval(t, journal, plan, now)
	for name, run := range map[string]func() error{
		"start-invalid": func() error { _, err := journal.StartOperation(OperationInput{}); return err },
		"start-missing": func() error {
			_, err := journal.StartOperation(OperationInput{ID: "operation-a", ApprovalID: "missing", PlanDigest: plan.Digest})
			return err
		},
		"start-plan-conflict": func() error {
			_, err := journal.StartOperation(OperationInput{ID: "operation-a", ApprovalID: "approval-a", PlanDigest: digestA})
			return err
		},
		"begin-invalid": func() error { _, err := journal.BeginStep(StepInput{}); return err },
		"begin-missing": func() error {
			_, err := journal.BeginStep(StepInput{OperationID: "missing", StepID: "step-a", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1})
			return err
		},
		"finish-invalid": func() error { _, err := journal.FinishStep(StepResultInput{}); return err },
		"finish-missing-operation": func() error {
			_, err := journal.FinishStep(StepResultInput{OperationID: "missing", StepID: "step-a", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA})
			return err
		},
		"cancel-invalid":   func() error { _, err := journal.Cancel("bad--id"); return err },
		"cancel-missing":   func() error { _, err := journal.Cancel("missing"); return err },
		"rollback-invalid": func() error { _, err := journal.BeginRollback("bad--id"); return err },
		"rollback-missing": func() error { _, err := journal.BeginRollback("missing"); return err },
		"terminal-invalid": func() error { _, err := journal.FinishOperation("bad--id", OutcomeComplete); return err },
		"terminal-missing": func() error { _, err := journal.FinishOperation("missing", OutcomeComplete); return err },
		"get-invalid":      func() error { _, err := journal.Operation("bad--id"); return err },
		"get-missing":      func() error { _, err := journal.Operation("missing"); return err },
	} {
		err := run()
		if err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	operation := startTestOperation(t, journal, plan)
	if _, err := journal.FinishOperation(operation.ID, OutcomeComplete); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("incomplete operation=%v", err)
	}
}
