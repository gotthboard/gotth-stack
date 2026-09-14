package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInputValidationBoundaries(t *testing.T) {
	now := time.Now().UTC()
	validApproval := ApprovalInput{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: digestA, ExpiresAt: now.Add(time.Hour), SecretRevisions: []SecretRevision{}}
	approvalCases := []ApprovalInput{
		{},
		{ID: "bad--id", ActorID: "actor-a", AuthorityDigest: digestA, ExpiresAt: now, SecretRevisions: []SecretRevision{}},
		{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: "bad", ExpiresAt: now, SecretRevisions: []SecretRevision{}},
		{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: digestA, SecretRevisions: []SecretRevision{}},
		{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: digestA, ExpiresAt: now, SecretRevisions: nil},
		{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: digestA, ExpiresAt: now, SecretRevisions: []SecretRevision{{ComponentID: "database", Slot: "password", RevisionDigest: "bad"}}},
		{ID: "approval-a", ActorID: "actor-a", AuthorityDigest: digestA, ExpiresAt: now, SecretRevisions: []SecretRevision{{ComponentID: "database", Slot: "password", RevisionDigest: digestA}, {ComponentID: "database", Slot: "password", RevisionDigest: digestB}}},
	}
	if err := validateApprovalInput(validApproval); err != nil {
		t.Fatalf("valid approval: %v", err)
	}
	for index, input := range approvalCases {
		if err := validateApprovalInput(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("approval case %d: %v", index, err)
		}
	}

	if err := validateOperationInput(OperationInput{ID: "operation-a", ApprovalID: "approval-a", PlanDigest: digestA}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []OperationInput{{}, {ID: "bad--id", ApprovalID: "approval-a", PlanDigest: digestA}, {ID: "operation-a", ApprovalID: "approval-a", PlanDigest: "bad"}} {
		if err := validateOperationInput(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("operation input=%#v err=%v", input, err)
		}
	}

	validSteps := []StepInput{
		{OperationID: "operation-a", StepID: "preflight-a", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1},
		{OperationID: "operation-a", StepID: "verify-a", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1},
		{OperationID: "operation-a", StepID: "apply-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}},
		{OperationID: "operation-a", StepID: "apply-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{RecoveryOnlyReason: RecoveryExternal}},
		{OperationID: "operation-a", StepID: "rollback-a", ComponentID: "database", Phase: PhaseRollback, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}, CompensatesStepID: "apply-a"},
	}
	for _, input := range validSteps {
		if err := validateStepInput(input); err != nil {
			t.Fatalf("valid step=%#v err=%v", input, err)
		}
	}
	invalidSteps := []StepInput{
		{},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhasePreflight, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeReadOnly, Attempt: 1},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: "invented", Mode: ModeReadOnly, Attempt: 1},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, Rollback: RollbackPolicy{ReferenceDigest: digestB}},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB, RecoveryOnlyReason: RecoveryExternal}},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1, IdempotencyDigest: digestA},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseRollback, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}},
		{OperationID: "operation-a", StepID: "step-a", ComponentID: "database", Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA, Rollback: RollbackPolicy{ReferenceDigest: digestB}, CompensatesStepID: "other"},
	}
	for index, input := range invalidSteps {
		if err := validateStepInput(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("step case %d err=%v", index, err)
		}
	}

	validResults := []StepResultInput{
		{OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA},
		{OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepFailed, ReasonCode: "connection_failed"},
	}
	for _, input := range validResults {
		if err := validateStepResultInput(input); err != nil {
			t.Fatalf("valid result=%#v err=%v", input, err)
		}
	}
	for _, input := range []StepResultInput{{}, {OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepSucceeded}, {OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA, ReasonCode: "bad"}, {OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepFailed, ResultDigest: digestA, ReasonCode: "bad"}, {OperationID: "operation-a", StepID: "step-a", Attempt: 1, Status: StepFailed, ReasonCode: "bad-value"}} {
		if err := validateStepResultInput(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("result=%#v err=%v", input, err)
		}
	}
}

func TestStrictJSONRejectsAmbiguity(t *testing.T) {
	valid := []byte(`{"schema_version":1,"sequence":0,"digest":"` + zeroDigest + `"}`)
	var head headFile
	if err := decodeStrict(valid, &head); err != nil {
		t.Fatal(err)
	}
	if err := decodeCanonicalDocument(append(valid, '\n'), &head); err != nil {
		t.Fatal(err)
	}
	if err := decodeCanonicalDocument(valid, &head); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("noncanonical document=%v", err)
	}
	invalid := [][]byte{
		[]byte(`{"schema_version":1,"schema_version":1,"sequence":0,"digest":"` + zeroDigest + `"}`),
		[]byte(`{"schema_version":1,"sequence":0,"digest":"` + zeroDigest + `","unknown":1}`),
		[]byte(`{"schema_version":1,"sequence":0,"digest":"` + zeroDigest + `"} {}`),
		[]byte(`[` + strings.Repeat(`[`, maxJSONDepth) + strings.Repeat(`]`, maxJSONDepth) + `]`),
		[]byte(`{"schema_version":`),
		[]byte(`{"schema_version":[],"sequence":0,"digest":"` + zeroDigest + `"}`),
	}
	for index, raw := range invalid {
		if err := decodeStrict(raw, &head); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("case %d err=%v", index, err)
		}
	}
	if err := inspectJSONValue(jsonDecoder(`}`), 0); err == nil {
		t.Fatal("accepted closing delimiter as a value")
	}
}

func jsonDecoder(value string) *json.Decoder { return json.NewDecoder(bytes.NewBufferString(value)) }

func TestOpenRejectsIdentityHeadAndDirectoryCorruption(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"identity": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "installation.json"), []byte("{\"schema_version\":1,\"installation_id\":\"other\"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"head-ahead": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "journal.head"), []byte("{\"schema_version\":1,\"sequence\":1,\"digest\":\""+digestA+"\"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"head-invalid": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "journal.head"), []byte("{\"schema_version\":1,\"sequence\":0,\"digest\":\""+digestA+"\"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unknown-file": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "garbage"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"missing-file": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "journal.head")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			journal, root, _ := newTestJournal(t)
			_ = journal.Close()
			mutate(t, root)
			if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
