package journal

import (
	"errors"
	"testing"
	"time"
)

func TestRecordShapeRejectsEveryAmbiguity(t *testing.T) {
	now := time.Now().UTC()
	approval := &Approval{}
	operation := &operationRecord{}
	base := journalRecord{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval, Approval: approval}
	if err := base.validateShape(); err != nil {
		t.Fatalf("valid shape: %v", err)
	}
	cases := []journalRecord{
		{},
		{SchemaVersion: 2, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 2, PreviousDigest: "bad", InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: digestA, InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordApproval, Approval: approval, Operation: operation},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordOperation, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordStepStart, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordStepFinish, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordCancel, OperationID: "bad--id"},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: recordOperationFinish, Approval: approval},
		{SchemaVersion: SchemaVersion, Sequence: 1, PreviousDigest: zeroDigest, InstallationID: "installation-a", ObservedAt: now, Kind: "invented", Approval: approval},
	}
	for index, record := range cases {
		if err := record.validateShape(); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("case %d err=%v", index, err)
		}
	}
}

func TestStoredModelValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := Approval{
		SchemaVersion: SchemaVersion, ID: "approval-a", InstallationID: "installation-a",
		PlanDigest: digestA, ManifestDigest: digestB, ActorID: "actor-a", AuthorityDigest: digestC,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		Components:      []ApprovedComponent{{Index: 1, ComponentID: "database", Adapter: "gotth-stack-adapter-postgresql.v1", ArtifactDigest: digestA, ConfigurationDigest: digestB, Capabilities: []string{"database.store"}}},
		SecretRevisions: []SecretRevision{},
	}
	if err := validateStoredApproval(valid, "installation-a"); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Approval){
		func(value *Approval) { value.SchemaVersion = 2 },
		func(value *Approval) { value.Components = nil },
		func(value *Approval) { value.Components[0].Index = 2 },
		func(value *Approval) { value.Components[0].Adapter = "bad" },
		func(value *Approval) { value.Components[0].Capabilities = []string{"bad value"} },
		func(value *Approval) { value.Components = append(value.Components, value.Components[0]) },
		func(value *Approval) {
			value.SecretRevisions = []SecretRevision{{ComponentID: "other", Slot: "password", RevisionDigest: digestA}}
		},
		func(value *Approval) {
			value.SecretRevisions = []SecretRevision{{ComponentID: "database", Slot: "password", RevisionDigest: digestA}, {ComponentID: "database", Slot: "password", RevisionDigest: digestB}}
		},
	}
	for index, mutate := range mutations {
		candidate := cloneApproval(valid)
		mutate(&candidate)
		if err := validateStoredApproval(candidate, "installation-a"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d err=%v", index, err)
		}
	}

	validStep := Step{OperationID: "operation-a", StepID: "preflight-a", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1, StartedAt: now, Status: StepRunning}
	if err := validateStoredStep(validStep); err != nil {
		t.Fatal(err)
	}
	validStep.FinishedAt = now
	if err := validateStoredStep(validStep); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("finished start=%v", err)
	}
	validStep.FinishedAt = time.Time{}
	validStep.RetryOfInterrupted = true
	if err := validateStoredStep(validStep); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("attempt-one retry=%v", err)
	}
}

func TestApplyRecordRejectsUnknownAndConflictingReplay(t *testing.T) {
	journal := &Journal{installationID: "installation-a", approvals: map[string]Approval{}, operations: map[string]*Operation{}}
	if err := journal.applyRecord(journalRecord{Kind: "invented"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unknown=%v", err)
	}
	if err := journal.applyOperation(operationRecord{ID: "operation-a", ApprovalID: "missing", PlanDigest: digestA}, time.Now().UTC()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("missing approval=%v", err)
	}
	if err := journal.applyStepFinish(stepFinishRecord{OperationID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing finish=%v", err)
	}
	if err := journal.applyCancel("missing", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cancel=%v", err)
	}
	if err := journal.applyRollback("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rollback=%v", err)
	}
	if err := journal.applyTerminal(terminalRecord{OperationID: "missing", Outcome: OutcomeFailed}, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing terminal=%v", err)
	}
}

func TestCanCompleteAndRollbackRejectIncompleteState(t *testing.T) {
	approval := Approval{Components: []ApprovedComponent{{ComponentID: "database"}}}
	operation := &Operation{State: StateVerifying, Steps: []Step{}}
	if canComplete(operation, approval) {
		t.Fatal("completed without verification")
	}
	operation.Failure = &FailurePoint{}
	if canComplete(operation, approval) {
		t.Fatal("completed with failure")
	}
	operation.Failure = nil
	operation.Steps = []Step{{ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Status: StepSucceeded}, {ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Status: StepFailed}}
	if canComplete(operation, approval) {
		t.Fatal("completed with unresolved mutation")
	}

	operation = &Operation{State: StateRollingBack, RollbackStarted: true, Steps: []Step{{StepID: "apply-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Status: StepSucceeded, Rollback: RollbackPolicy{ReferenceDigest: digestA}}}}
	if canReportRolledBack(operation) {
		t.Fatal("rolled back without compensation")
	}
	operation.Steps = append(operation.Steps, Step{StepID: "rollback-a", ComponentID: "database", Phase: PhaseRollback, Mode: ModeMutation, Status: StepFailed, CompensatesStepID: "apply-a"})
	if canReportRolledBack(operation) {
		t.Fatal("rolled back after rollback failure")
	}
}

func TestCanCompleteRequiresEveryApprovedComponent(t *testing.T) {
	approval := Approval{Components: []ApprovedComponent{{ComponentID: "database"}, {ComponentID: "proxy"}}}
	operation := &Operation{State: StateVerifying, Steps: []Step{{ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Status: StepSucceeded}}}
	if canComplete(operation, approval) {
		t.Fatal("completed with one approved component unverified")
	}
	operation.Steps = append(operation.Steps, Step{ComponentID: "proxy", Phase: PhaseVerify, Mode: ModeReadOnly, Status: StepSucceeded})
	if !canComplete(operation, approval) {
		t.Fatal("did not complete with every approved component verified")
	}
}
