package journal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gotthboard/gotth-stack/pkg/stack"
)

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestApprovalNoOpOperationAndReopen(t *testing.T) {
	journal, root, now := newTestJournal(t)
	plan := testPlan(t, true)
	approval := recordTestApproval(t, journal, plan, now)
	if approval.PlanDigest != plan.Digest || approval.Components[0].ComponentID != "database" {
		t.Fatalf("approval projection mismatch: %#v", approval)
	}
	duplicate := recordTestApproval(t, journal, plan, now)
	if duplicate.ID != approval.ID || journal.recordCount != 1 {
		t.Fatalf("duplicate approval appended or changed: count=%d", journal.recordCount)
	}
	operation := startTestOperation(t, journal, plan)
	operation = finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	operation = finishReadOnly(t, journal, operation.ID, "verify-database", "database", PhaseVerify, 1)
	operation, err := journal.FinishOperation(operation.ID, OutcomeComplete)
	if err != nil || operation.State != StateComplete {
		t.Fatalf("complete operation: state=%q err=%v", operation.State, err)
	}
	operation.Steps[0].ReasonCode = "caller-mutation"
	stored, err := journal.Operation(operation.ID)
	if err != nil || stored.Steps[0].ReasonCode != "" {
		t.Fatalf("returned operation was not a deep copy: %#v err=%v", stored, err)
	}
	if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open error=%v, want locked", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, recovery, err := Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if recovery.RecordCount != 7 || recovery.HeadRepaired || recovery.TruncatedBytes != 0 {
		t.Fatalf("unexpected recovery: %#v", recovery)
	}
	stored, err = reopened.Operation(operation.ID)
	if err != nil || stored.State != StateComplete {
		t.Fatalf("replayed state=%q err=%v", stored.State, err)
	}
	active, found, err := reopened.ActiveOperation()
	if err != nil || found {
		t.Fatalf("terminal operation reported active=%#v found=%v err=%v", active, found, err)
	}
	replayedApproval, err := reopened.Approval(operation.ApprovalID)
	if err != nil || replayedApproval.ID != approval.ID {
		t.Fatalf("replayed approval=%#v err=%v", replayedApproval, err)
	}
	replayedApproval.Components[0].Capabilities[0] = "caller.mutation"
	again, err := reopened.Approval(approval.ID)
	if err != nil || again.Components[0].Capabilities[0] == "caller.mutation" {
		t.Fatalf("approval was not a deep copy: %#v err=%v", again, err)
	}
}

func TestJournalLockAcrossProcesses(t *testing.T) {
	journal, root, _ := newTestJournal(t)
	command := exec.Command(os.Args[0], "-test.run=^TestJournalLockHelperProcess$")
	command.Env = append(os.Environ(), "GOTTH_STACK_LOCK_HELPER="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v: %s", err, output)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(os.Args[0], "-test.run=^TestJournalLockHelperProcess$")
	command.Env = append(os.Environ(), "GOTTH_STACK_LOCK_HELPER="+root, "GOTTH_STACK_LOCK_EXPECT_OPEN=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper open failed: %v: %s", err, output)
	}
}

func TestJournalLockHelperProcess(t *testing.T) {
	root := os.Getenv("GOTTH_STACK_LOCK_HELPER")
	if root == "" {
		return
	}
	journal, _, err := Open(root, "installation-a")
	if os.Getenv("GOTTH_STACK_LOCK_EXPECT_OPEN") == "1" {
		if err != nil {
			t.Fatalf("open after release: %v", err)
		}
		_ = journal.Close()
		return
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("lock error=%v", err)
	}
}

func TestInterruptedReadOnlyRequiresExplicitRetry(t *testing.T) {
	journal, root, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	_, err := journal.BeginStep(StepInput{OperationID: operation.ID, StepID: "preflight-database", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	reopened, _, err := Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	operation, _ = reopened.Operation(operation.ID)
	if operation.State != StateRetryRequired || !operation.Steps[0].Recovered {
		t.Fatalf("state=%q step=%#v", operation.State, operation.Steps[0])
	}
	if _, err := reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "preflight-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("interrupted read-only finish error=%v", err)
	}
	if _, err := reopened.BeginStep(StepInput{OperationID: operation.ID, StepID: "other", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unrelated retry error=%v", err)
	}
	if _, err := reopened.BeginStep(StepInput{OperationID: operation.ID, StepID: "preflight-database", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 2}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("phase-changing retry error=%v", err)
	}
	operation, err = reopened.BeginStep(StepInput{OperationID: operation.ID, StepID: "preflight-database", ComponentID: "database", Phase: PhasePreflight, Mode: ModeReadOnly, Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Steps[0].ReasonCode != "interrupted_retry" || !operation.Steps[1].RetryOfInterrupted {
		t.Fatalf("retry history=%#v", operation.Steps)
	}
	if _, err := reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "preflight-database", Attempt: 2, Status: StepSucceeded, ResultDigest: digestB}); err != nil {
		t.Fatal(err)
	}
	_ = reopened.Close()
	replayed, _, err := Open(root, "installation-a")
	if err != nil {
		t.Fatalf("retry did not replay: %v", err)
	}
	_ = replayed.Close()
}

func TestInterruptedMutationRequiresReconciliationAndRollback(t *testing.T) {
	journal, root, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	finishReadOnly(t, journal, operation.ID, "preflight-database", "database", PhasePreflight, 1)
	operation, err := journal.BeginStep(StepInput{
		OperationID: operation.ID, StepID: "apply-database", ComponentID: "database", Phase: PhaseApply,
		Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestB,
		Rollback: RollbackPolicy{ReferenceDigest: digestC},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	reopened, _, err := Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	operation, _ = reopened.Operation(operation.ID)
	if operation.State != StateRecoveryRequired || !operation.Steps[len(operation.Steps)-1].Recovered {
		t.Fatalf("state=%q steps=%#v", operation.State, operation.Steps)
	}
	if _, err := reopened.Cancel(operation.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancel error=%v", err)
	}
	operation, err = reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "apply-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestA})
	if err != nil || operation.State != StateApplying {
		t.Fatalf("reconcile state=%q err=%v", operation.State, err)
	}
	_ = reopened.Close()
	reopened, _, err = Open(root, "installation-a")
	if err != nil {
		t.Fatalf("reconciled mutation replay: %v", err)
	}
	operation, _ = reopened.Operation(operation.ID)
	if operation.State != StateApplying || operation.Failure != nil {
		t.Fatalf("reconciled replay=%#v", operation)
	}
	if _, err := reopened.BeginStep(StepInput{OperationID: operation.ID, StepID: "verify-database", ComponentID: "database", Phase: PhaseVerify, Mode: ModeReadOnly, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	operation, err = reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "verify-database", Attempt: 1, Status: StepFailed, ReasonCode: "verify_failed"})
	if err != nil || operation.State != StateFailed {
		t.Fatalf("verify failure=%#v err=%v", operation, err)
	}
	operation, err = reopened.BeginRollback(operation.ID)
	if err != nil || operation.State != StateRollingBack {
		t.Fatalf("begin rollback state=%q err=%v", operation.State, err)
	}
	operation, err = reopened.BeginStep(StepInput{
		OperationID: operation.ID, StepID: "rollback-database", ComponentID: "database", Phase: PhaseRollback,
		Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA,
		Rollback: RollbackPolicy{ReferenceDigest: digestC}, CompensatesStepID: "apply-database",
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = reopened.FinishStep(StepResultInput{OperationID: operation.ID, StepID: "rollback-database", Attempt: 1, Status: StepSucceeded, ResultDigest: digestB})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = reopened.FinishOperation(operation.ID, OutcomeRolledBack)
	if err != nil || operation.State != StateRolledBack {
		t.Fatalf("rolled back state=%q err=%v", operation.State, err)
	}
}

func TestApprovalExpiryConflictsAndCancellation(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	if _, err := journal.RecordApproval(plan, ApprovalInput{ID: "approval-a", ActorID: "different", AuthorityDigest: digestA, ExpiresAt: now.Add(time.Hour), SecretRevisions: []SecretRevision{}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error=%v", err)
	}
	journal.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := journal.StartOperation(OperationInput{ID: "late-operation", ApprovalID: "approval-a", PlanDigest: plan.Digest}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry error=%v", err)
	}
	journal.now = func() time.Time { return now }
	operation := startTestOperation(t, journal, plan)
	operation, err := journal.Cancel(operation.ID)
	if err != nil || operation.State != StateCancelled {
		t.Fatalf("cancel state=%q err=%v", operation.State, err)
	}
	if _, err := journal.Cancel(operation.ID); err != nil {
		t.Fatalf("duplicate cancel: %v", err)
	}
}

func TestTornTailAndStaleHeadRecovery(t *testing.T) {
	t.Run("torn-tail", func(t *testing.T) {
		journal, root, now := newTestJournal(t)
		recordTestApproval(t, journal, testPlan(t, false), now)
		_ = journal.Close()
		logPath := filepath.Join(root, "journal.log")
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte("GTSJ"))
		_ = file.Close()
		reopened, recovery, err := Open(root, "installation-a")
		if err != nil {
			t.Fatal(err)
		}
		_ = reopened.Close()
		if recovery.TruncatedBytes != 4 || recovery.RecordCount != 1 {
			t.Fatalf("recovery=%#v", recovery)
		}
	})

	t.Run("stale-head", func(t *testing.T) {
		journal, root, now := newTestJournal(t)
		originalRename := journal.ops.rename
		journal.ops.rename = func(*os.Root, string, string) error { return errors.New("injected") }
		_, err := journal.RecordApproval(testPlan(t, false), ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(time.Hour), SecretRevisions: []SecretRevision{}})
		if !errors.Is(err, ErrStorage) {
			t.Fatalf("append error=%v", err)
		}
		journal.ops.rename = originalRename
		_ = journal.Close()
		reopened, recovery, err := Open(root, "installation-a")
		if err != nil {
			t.Fatal(err)
		}
		_ = reopened.Close()
		if !recovery.HeadRepaired || recovery.RecordCount != 1 {
			t.Fatalf("recovery=%#v", recovery)
		}
	})
}

func TestCommittedCorruptionAndStoragePoisonFailClosed(t *testing.T) {
	journal, root, now := newTestJournal(t)
	recordTestApproval(t, journal, testPlan(t, false), now)
	originalSync := journal.ops.sync
	journal.ops.sync = func(*os.File) error { return errors.New("injected") }
	if _, err := journal.StartOperation(OperationInput{ID: "operation-a", ApprovalID: "approval-a", PlanDigest: testPlan(t, false).Digest}); !errors.Is(err, ErrStorage) {
		t.Fatalf("storage error=%v", err)
	}
	journal.ops.sync = originalSync
	if _, err := journal.Operation("operation-a"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("poison error=%v", err)
	}
	_ = journal.Close()

	data, err := os.ReadFile(filepath.Join(root, "journal.log"))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(filepath.Join(root, "journal.log"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corruption error=%v", err)
	}
}

func TestFilesystemContractRejectsBroadModesAndSymlinks(t *testing.T) {
	t.Run("broad-mode", func(t *testing.T) {
		journal, root, _ := newTestJournal(t)
		_ = journal.Close()
		if err := os.Chmod(filepath.Join(root, "journal.log"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("mode error=%v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		journal, root, _ := newTestJournal(t)
		_ = journal.Close()
		if err := os.Remove(filepath.Join(root, "journal.log")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("installation.json", filepath.Join(root, "journal.log")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("symlink error=%v", err)
		}
	})
}

func newTestJournal(t *testing.T) (*Journal, string, time.Time) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "journal")
	journal, err := Create(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	t.Cleanup(func() { _ = journal.Close() })
	return journal, root, now
}

func testPlan(t *testing.T, withSecret bool) stack.Plan {
	t.Helper()
	secrets := []string{}
	if withSecret {
		secrets = []string{"database-password"}
	}
	plan, err := stack.BuildPlan(stack.Manifest{
		SchemaVersion: stack.SchemaVersion, Name: "test-stack",
		Components: []stack.Component{{
			ID: "database", Adapter: "gotth-stack-adapter-postgresql.v1",
			Artifact:            stack.Artifact{Source: "oci://registry.example.test/postgresql", Digest: digestA},
			ConfigurationDigest: digestB, DependsOn: []string{}, Capabilities: []string{"database.store"}, SecretSlots: secrets,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func recordTestApproval(t *testing.T, journal *Journal, plan stack.Plan, now time.Time) Approval {
	t.Helper()
	revisions := []SecretRevision{}
	if len(plan.Steps[0].SecretSlots) > 0 {
		revisions = []SecretRevision{{ComponentID: "database", Slot: "database-password", RevisionDigest: digestC}}
	}
	approval, err := journal.RecordApproval(plan, ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(time.Hour), SecretRevisions: revisions})
	if err != nil {
		t.Fatal(err)
	}
	return approval
}

func startTestOperation(t *testing.T, journal *Journal, plan stack.Plan) Operation {
	t.Helper()
	operation, err := journal.StartOperation(OperationInput{ID: "operation-a", ApprovalID: "approval-a", PlanDigest: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func finishReadOnly(t *testing.T, journal *Journal, operationID, stepID, componentID string, phase Phase, attempt uint32) Operation {
	t.Helper()
	if _, err := journal.BeginStep(StepInput{OperationID: operationID, StepID: stepID, ComponentID: componentID, Phase: phase, Mode: ModeReadOnly, Attempt: attempt}); err != nil {
		t.Fatal(err)
	}
	operation, err := journal.FinishStep(StepResultInput{OperationID: operationID, StepID: stepID, Attempt: attempt, Status: StepSucceeded, ResultDigest: digestA})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func TestErrorsDoNotDiscloseHostileInput(t *testing.T) {
	journal, _, _ := newTestJournal(t)
	hostile := "secret-" + strings.Repeat("x", 200)
	_, err := journal.Operation(hostile)
	if err == nil || strings.Contains(err.Error(), hostile) {
		t.Fatalf("error disclosed input: %v", err)
	}
}

func TestLargestValidPlanFitsOneApprovalRecord(t *testing.T) {
	journal, _, now := newTestJournal(t)
	components := make([]stack.Component, 64)
	revisions := make([]SecretRevision, 0, 64*64)
	for componentIndex := range components {
		componentID := paddedSlug("component", componentIndex, 80)
		capabilities := make([]string, 64)
		secretSlots := make([]string, 64)
		for valueIndex := 0; valueIndex < 64; valueIndex++ {
			capabilities[valueIndex] = paddedCapability(valueIndex, 160)
			secretSlots[valueIndex] = paddedSlug("secret", valueIndex, 80)
			revisions = append(revisions, SecretRevision{ComponentID: componentID, Slot: secretSlots[valueIndex], RevisionDigest: digestC})
		}
		adapterPrefix := "gotth-stack-adapter-"
		adapterSuffix := ".v1"
		components[componentIndex] = stack.Component{
			ID:      componentID,
			Adapter: adapterPrefix + paddedSlug("adapter", componentIndex, 160-len(adapterPrefix)-len(adapterSuffix)) + adapterSuffix,
			Artifact: stack.Artifact{
				Source: "https://registry.example.test/" + strings.Repeat("a", 2048-len("https://registry.example.test/")),
				Digest: digestA,
			},
			ConfigurationDigest: digestB,
			DependsOn:           []string{},
			Capabilities:        capabilities,
			SecretSlots:         secretSlots,
		}
	}
	plan, err := stack.BuildPlan(stack.Manifest{SchemaVersion: stack.SchemaVersion, Name: "largest-stack", Components: components})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(revisions, func(left, right int) bool { return secretKey(revisions[left]) < secretKey(revisions[right]) })
	if _, err := journal.RecordApproval(plan, ApprovalInput{ID: "approval-largest", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(time.Hour), SecretRevisions: revisions}); err != nil {
		t.Fatalf("largest bounded approval: %v", err)
	}
	info, err := journal.log.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 1<<20 || info.Size() > int64(MaxPayloadSize+frameHeaderSize) {
		t.Fatalf("largest approval frame size=%d", info.Size())
	}
}

func TestRestartQueriesExposeOnlyActiveAuthority(t *testing.T) {
	journal, _, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	active, found, err := journal.ActiveOperation()
	if err != nil || !found || active.ID != operation.ID {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
	active.State = StateComplete
	again, found, err := journal.ActiveOperation()
	if err != nil || !found || again.State != StateApproved {
		t.Fatalf("active operation was not a deep copy: %#v found=%v err=%v", again, found, err)
	}
	if _, err := journal.Approval("bad--id"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid approval lookup=%v", err)
	}
	if _, err := journal.Approval("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing approval lookup=%v", err)
	}
}

func paddedSlug(prefix string, index, length int) string {
	suffix := fmt.Sprintf("-%02d", index)
	return prefix + "-" + strings.Repeat("a", length-len(prefix)-len(suffix)-1) + suffix
}

func paddedCapability(index, length int) string {
	prefix := fmt.Sprintf("capability-%02d.", index)
	return prefix + strings.Repeat("a", length-len(prefix))
}
