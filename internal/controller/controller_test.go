package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotthboard/gotth-stack/pkg/journal"
	"github.com/gotthboard/gotth-stack/pkg/stack"
)

type fakeState struct {
	State             int    `json:"state"`
	RollbackReference string `json:"rollback_reference"`
}

type fakeMechanism struct {
	state               int
	failAction          string
	unknown             bool
	unknownState        int
	crashAfterAction    bool
	crashAction         string
	crashPreflightOnce  bool
	observationFailures int
	label               string
	events              *[]string
	journal             *journal.Journal
	operationID         string
	t                   *testing.T
}

func (fake *fakeMechanism) observation() []byte {
	value, err := json.Marshal(fakeState{State: fake.state, RollbackReference: testDigest("rollback")})
	if err != nil {
		fake.t.Fatal(err)
	}
	return value
}

func (fake *fakeMechanism) mutate(name string, target int) func(context.Context, string) error {
	return func(_ context.Context, operationID string) error {
		if fake.events != nil {
			*fake.events = append(*fake.events, fake.label+":"+name)
		}
		operation, err := fake.journal.Operation(operationID)
		if err != nil || len(operation.Steps) == 0 || operation.Steps[len(operation.Steps)-1].Status != journal.StepRunning {
			fake.t.Fatalf("effect ran without durable running intent: operation=%#v err=%v", operation, err)
		}
		if fake.failAction == name {
			if fake.unknown {
				fake.state = fake.unknownState
				if fake.state == 0 {
					fake.state = 99
				}
			}
			if fake.crashAfterAction {
				fake.observationFailures++
			}
			return errors.New("injected")
		}
		fake.state = target
		if fake.crashAction == name {
			fake.observationFailures++
		}
		return nil
	}
}

func fakeBinding(t *testing.T, j *journal.Journal, mechanism *fakeMechanism) *Binding {
	t.Helper()
	mechanism.journal = j
	mechanism.t = t
	observe := func(context.Context, string) ([]byte, error) {
		if mechanism.observationFailures > 0 {
			mechanism.observationFailures--
			return nil, errors.New("injected observation failure")
		}
		return mechanism.observation(), nil
	}
	reached := func(want int) func([]byte) bool {
		return func(value []byte) bool {
			var state fakeState
			return json.Unmarshal(value, &state) == nil && state.State == want
		}
	}
	return &Binding{
		componentID: "proxy", adapterID: AdapterCaddy,
		capabilities: []string{"configuration.replace", "runtime.reload"}, secretSlots: []string{}, secretDigests: map[string]string{},
		artifactDigest: testDigest("artifact"), configurationDigest: testDigest("configuration"),
		observe: observe,
		preflight: func(_ context.Context, operationID string) ([]byte, error) {
			operation, err := mechanism.journal.Operation(operationID)
			if err != nil || len(operation.Steps) == 0 || operation.Steps[len(operation.Steps)-1].Status != journal.StepRunning {
				t.Fatalf("preflight ran without durable intent: operation=%#v err=%v", operation, err)
			}
			if mechanism.crashPreflightOnce {
				mechanism.crashPreflightOnce = false
				mechanism.observationFailures++
			}
			return []byte("prepared"), nil
		},
		stage: mechanism.mutate("stage", 1), reconcileStage: mechanism.mutate("reconcile-stage", 1), stageReached: reached(1),
		rollbackReference: func(value []byte) (string, error) {
			var state fakeState
			if json.Unmarshal(value, &state) != nil || state.RollbackReference == "" {
				return "", ErrInvalidInput
			}
			return state.RollbackReference, nil
		},
		forward: []transition{
			{name: "install", call: mechanism.mutate("install", 2), reverse: mechanism.mutate("restore", 1), reached: reached(2), predecessor: reached(1), reversed: reached(1)},
			{name: "activate", call: mechanism.mutate("activate", 3), reverse: mechanism.mutate("reactivate", 2), reached: reached(3), predecessor: reached(2), reversed: reached(2)},
		},
		rollbackOrder:   []int{1, 0},
		verifyCandidate: transition{name: "verify-candidate", call: mechanism.mutate("verify-candidate", 3), reached: reached(3)},
		verifyPrevious:  transition{name: "verify-previous", call: mechanism.mutate("verify-previous", 1), reached: reached(1)},
	}
}

func newControllerFixture(t *testing.T, mechanism *fakeMechanism) (*Controller, *journal.Journal, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "journal")
	j, err := journal.Create(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	manifest := stack.Manifest{SchemaVersion: stack.SchemaVersion, Name: "test-stack", Components: []stack.Component{{
		ID: "proxy", Adapter: AdapterCaddy, Artifact: stack.Artifact{Source: "oci://example.test/proxy", Digest: testDigest("artifact")},
		ConfigurationDigest: testDigest("configuration"), Capabilities: []string{"configuration.replace", "runtime.reload"}, SecretSlots: []string{}, DependsOn: []string{},
	}}}
	plan, err := stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	binding := fakeBinding(t, j, mechanism)
	controller, err := New(j, plan, journal.ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: binding})
	if err != nil {
		t.Fatal(err)
	}
	return controller, j, root
}

func TestExecuteWritesIntentBeforeEveryEffect(t *testing.T) {
	mechanism := &fakeMechanism{}
	controller, _, _ := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if err != nil || operation.State != journal.StateComplete || mechanism.state != 3 {
		t.Fatalf("operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	if len(operation.Steps) != 5 {
		t.Fatalf("steps=%d, want 5", len(operation.Steps))
	}
}

func TestExecuteExactDuplicateReturnsDurableTerminalResult(t *testing.T) {
	mechanism := &fakeMechanism{}
	controller, _, _ := newControllerFixture(t, mechanism)
	first, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if err != nil || first.State != journal.StateComplete {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if err != nil || second.State != journal.StateComplete || len(second.Steps) != len(first.Steps) {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestExecuteExactDuplicatePreservesFailureOutcome(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "activate"}
	controller, _, _ := newControllerFixture(t, mechanism)
	first, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrAdapter) || first.State != journal.StateRolledBack {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrAdapter) || second.State != journal.StateRolledBack || len(second.Steps) != len(first.Steps) {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestKnownApplyFailureRollsBackInReverseAndVerifies(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "activate"}
	controller, _, _ := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrAdapter) || operation.State != journal.StateRolledBack || mechanism.state != 1 {
		t.Fatalf("operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	if !operation.RollbackStarted {
		t.Fatal("rollback was not recorded")
	}
	last := operation.Steps[len(operation.Steps)-1]
	if last.Phase != journal.PhaseRollback || last.Mode != journal.ModeReadOnly || last.Status != journal.StepSucceeded {
		t.Fatalf("last step=%#v", last)
	}
}

func TestDependentComponentsRollbackInReversePlanOrder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "journal")
	j, err := journal.Create(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	events := []string{}
	proxyMechanism := &fakeMechanism{label: "proxy", events: &events}
	databaseMechanism := &fakeMechanism{label: "database", events: &events, failAction: "activate"}
	proxy := fakeBinding(t, j, proxyMechanism)
	proxy.artifactDigest = testDigest("proxy-artifact")
	proxy.configurationDigest = testDigest("proxy-configuration")
	database := fakeBinding(t, j, databaseMechanism)
	database.componentID = "database"
	database.adapterID = AdapterPostgreSQL
	database.capabilities = []string{"runtime.replace"}
	database.artifactDigest = testDigest("database-artifact")
	database.configurationDigest = testDigest("database-configuration")
	manifest := stack.Manifest{SchemaVersion: stack.SchemaVersion, Name: "test-stack", Components: []stack.Component{
		{ID: "proxy", Adapter: AdapterCaddy, Artifact: stack.Artifact{Source: "oci://example.test/proxy", Digest: proxy.artifactDigest}, ConfigurationDigest: proxy.configurationDigest, Capabilities: proxy.capabilities, SecretSlots: []string{}, DependsOn: []string{}},
		{ID: "database", Adapter: AdapterPostgreSQL, Artifact: stack.Artifact{Source: "oci://example.test/database", Digest: database.artifactDigest}, ConfigurationDigest: database.configurationDigest, Capabilities: database.capabilities, SecretSlots: []string{}, DependsOn: []string{"proxy"}},
	}}
	plan, err := stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := New(j, plan, journal.ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: proxy, PostgreSQL: database})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrAdapter) || operation.State != journal.StateRolledBack || proxyMechanism.state != 1 || databaseMechanism.state != 1 {
		t.Fatalf("operation=%#v proxy=%d database=%d err=%v", operation, proxyMechanism.state, databaseMechanism.state, err)
	}
	position := func(want string) int {
		for index, event := range events {
			if event == want {
				return index
			}
		}
		return -1
	}
	databaseRollback := position("database:reactivate")
	proxyRollback := position("proxy:reactivate")
	if databaseRollback < 0 || proxyRollback < 0 || databaseRollback >= proxyRollback {
		t.Fatalf("rollback order=%v", events)
	}
}

func TestRecoveryOnlyMutationCompletesOrFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "failed"}[fail], func(t *testing.T) {
			mechanism := &fakeMechanism{}
			if fail {
				mechanism.failAction = "ensure"
			}
			controller, _, _ := newControllerFixture(t, mechanism)
			binding := controller.ordered[0]
			reached := func(want int) func([]byte) bool {
				return func(value []byte) bool {
					var state fakeState
					return json.Unmarshal(value, &state) == nil && state.State == want
				}
			}
			binding.forward = []transition{{name: "ensure", call: mechanism.mutate("ensure", 2), reached: reached(2), predecessor: reached(1), recoveryOnly: journal.RecoveryNoRollback}}
			binding.rollbackOrder = []int{}
			binding.verifyCandidate = transition{name: "verify", call: mechanism.mutate("verify", 2), reached: reached(2)}
			operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
			if !fail && (err != nil || operation.State != journal.StateComplete) {
				t.Fatalf("operation=%#v err=%v", operation, err)
			}
			if fail && (!errors.Is(err, ErrRecoveryRequired) || operation.State != journal.StateRecoveryRequired) {
				t.Fatalf("operation=%#v err=%v", operation, err)
			}
		})
	}
}

func TestVerificationFailureIsNotMaskedByCandidateState(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "verify-candidate"}
	controller, _, _ := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrAdapter) || operation.State != journal.StateRolledBack || mechanism.state != 1 {
		t.Fatalf("operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	verify := operation.Steps[4]
	if verify.Phase != journal.PhaseVerify || verify.Status != journal.StepFailed || verify.ReasonCode != "adapter_failed" {
		t.Fatalf("verify=%#v", verify)
	}
}

func TestRecoveryOnlyVerificationFailureRequiresRecovery(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "verify"}
	controller, _, _ := newControllerFixture(t, mechanism)
	binding := controller.ordered[0]
	reached := func(want int) func([]byte) bool {
		return func(value []byte) bool {
			var state fakeState
			return json.Unmarshal(value, &state) == nil && state.State == want
		}
	}
	binding.forward = []transition{{name: "ensure", call: mechanism.mutate("ensure", 2), reached: reached(2), predecessor: reached(1), recoveryOnly: journal.RecoveryNoRollback}}
	binding.rollbackOrder = []int{}
	binding.verifyCandidate = transition{name: "verify", call: mechanism.mutate("verify", 2), reached: reached(2)}
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || operation.State != journal.StateRecoveryRequired || operation.FinishedAt.IsZero() {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestRegistryRejectsMixedRecoveryPolicies(t *testing.T) {
	rollbackable := &Binding{forward: []transition{{recoveryOnly: ""}}}
	recoveryOnly := &Binding{forward: []transition{{recoveryOnly: journal.RecoveryNoRollback}}}
	if recoveryPolicyIsolated([]*Binding{rollbackable, recoveryOnly}) {
		t.Fatal("mixed rollback and recovery-only policy was admitted")
	}
	if !recoveryPolicyIsolated([]*Binding{rollbackable}) || !recoveryPolicyIsolated([]*Binding{recoveryOnly}) {
		t.Fatal("single-policy operation was rejected")
	}
}

func TestUnknownMutationOutcomeStopsWithoutRetry(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "activate", unknown: true}
	controller, j, root := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || mechanism.state != 99 {
		t.Fatalf("operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	if closeErr := j.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	reopened, _, openErr := journal.Open(root, "installation-a")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer reopened.Close()
	recovered, _ := reopened.Operation("operation-a")
	if recovered.State != journal.StateRecoveryRequired || recovered.Steps[len(recovered.Steps)-1].Status != journal.StepRunning {
		t.Fatalf("recovered=%#v", recovered)
	}
}

func TestRecoveredMutationIsObservedNotRetried(t *testing.T) {
	mechanism := &fakeMechanism{failAction: "activate", unknown: true, unknownState: 3, crashAfterAction: true}
	controller, j, root := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || operation.Steps[len(operation.Steps)-1].Status != journal.StepRunning {
		t.Fatalf("interrupted operation=%#v err=%v", operation, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := journal.Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	binding := fakeBinding(t, reopened, mechanism)
	recovered, err := New(reopened, controller.plan, journal.ApprovalInput{
		ID: controller.approval.ID, ActorID: controller.approval.ActorID, AuthorityDigest: controller.approval.AuthorityDigest,
		ExpiresAt: controller.approval.ExpiresAt, SecretRevisions: []journal.SecretRevision{},
	}, Bindings{Caddy: binding})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = recovered.Recover(context.Background())
	if err != nil || operation.State != journal.StateComplete || mechanism.state != 3 {
		t.Fatalf("recovered operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	for _, step := range operation.Steps {
		if step.StepID == "apply-01-02-activate" && step.Attempt != 1 {
			t.Fatalf("mutation was retried: %#v", step)
		}
	}
}

func TestInterruptedStageIsReconciledAsReadOnlyRetry(t *testing.T) {
	mechanism := &fakeMechanism{crashAction: "stage"}
	controller, j, root := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || mechanism.state != 1 {
		t.Fatalf("interrupted stage operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := journal.Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	binding := fakeBinding(t, reopened, mechanism)
	recovered, err := New(reopened, controller.plan, journal.ApprovalInput{
		ID: controller.approval.ID, ActorID: controller.approval.ActorID, AuthorityDigest: controller.approval.AuthorityDigest,
		ExpiresAt: controller.approval.ExpiresAt, SecretRevisions: []journal.SecretRevision{},
	}, Bindings{Caddy: binding})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = recovered.Recover(context.Background())
	if err != nil || operation.State != journal.StateComplete {
		t.Fatalf("recovered stage operation=%#v err=%v", operation, err)
	}
	foundRetry := false
	for _, step := range operation.Steps {
		if step.StepID == "stage-01" && step.Attempt == 2 && step.RetryOfInterrupted && step.Status == journal.StepSucceeded {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatal("stage reconciliation retry was not durably recorded")
	}
}

func TestInterruptedPreflightIsRetriedAndRefreshedBeforeStage(t *testing.T) {
	mechanism := &fakeMechanism{crashPreflightOnce: true}
	controller, j, root := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || operation.State != journal.StatePreflighting {
		t.Fatalf("interrupted operation=%#v err=%v", operation, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := journal.Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	binding := fakeBinding(t, reopened, mechanism)
	recovered, err := New(reopened, controller.plan, journal.ApprovalInput{
		ID: controller.approval.ID, ActorID: controller.approval.ActorID, AuthorityDigest: controller.approval.AuthorityDigest,
		ExpiresAt: controller.approval.ExpiresAt, SecretRevisions: []journal.SecretRevision{},
	}, Bindings{Caddy: binding})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = recovered.Recover(context.Background())
	if err != nil || operation.State != journal.StateComplete {
		t.Fatalf("recovered operation=%#v err=%v", operation, err)
	}
	retried, refreshed := false, false
	for _, step := range operation.Steps {
		if step.StepID == "preflight-01" && step.Attempt == 2 && step.RetryOfInterrupted && step.Status == journal.StepSucceeded {
			retried = true
		}
		if step.StepID == "refresh-01-01" && step.Status == journal.StepSucceeded {
			refreshed = true
		}
	}
	if !retried || !refreshed {
		t.Fatalf("retried=%v refreshed=%v steps=%#v", retried, refreshed, operation.Steps)
	}
}

func TestInterruptedVerificationFailureRollsBackDuringRecovery(t *testing.T) {
	mechanism := &fakeMechanism{crashAction: "verify-candidate"}
	controller, j, root := newControllerFixture(t, mechanism)
	operation, err := controller.Execute(context.Background(), ExecuteInput{OperationID: "operation-a"})
	if !errors.Is(err, ErrRecoveryRequired) || operation.State != journal.StateVerifying {
		t.Fatalf("interrupted operation=%#v err=%v", operation, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := journal.Open(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	mechanism.crashAction = ""
	mechanism.failAction = "verify-candidate"
	binding := fakeBinding(t, reopened, mechanism)
	recovered, err := New(reopened, controller.plan, journal.ApprovalInput{
		ID: controller.approval.ID, ActorID: controller.approval.ActorID, AuthorityDigest: controller.approval.AuthorityDigest,
		ExpiresAt: controller.approval.ExpiresAt, SecretRevisions: []journal.SecretRevision{},
	}, Bindings{Caddy: binding})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = recovered.Recover(context.Background())
	if !errors.Is(err, ErrAdapter) || operation.State != journal.StateRolledBack || mechanism.state != 1 {
		t.Fatalf("recovered operation=%#v state=%d err=%v", operation, mechanism.state, err)
	}
	foundRetry := false
	for _, step := range operation.Steps {
		if step.StepID == "verify-01" && step.Attempt == 2 && step.RetryOfInterrupted && step.Status == journal.StepFailed {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatal("failed verification retry was not durably recorded")
	}
}

func TestRegistryRejectsCapabilityAndSecretDrift(t *testing.T) {
	root := filepath.Join(t.TempDir(), "journal")
	j, err := journal.Create(root, "installation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	mechanism := &fakeMechanism{}
	binding := fakeBinding(t, j, mechanism)
	manifest := stack.Manifest{SchemaVersion: stack.SchemaVersion, Name: "test-stack", Components: []stack.Component{{
		ID: "proxy", Adapter: AdapterCaddy, Artifact: stack.Artifact{Source: "oci://example.test/proxy", Digest: testDigest("artifact")}, ConfigurationDigest: testDigest("configuration"),
		Capabilities: []string{"shell.execute"}, SecretSlots: []string{}, DependsOn: []string{},
	}}}
	plan, err := stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(j, plan, journal.ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: binding})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("capability drift error=%v", err)
	}
	manifest.Components[0].Capabilities = []string{"configuration.replace"}
	plan, err = stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(j, plan, journal.ApprovalInput{ID: "approval-subset", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: binding})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("partial capability set error=%v", err)
	}
	manifest.Components[0].Capabilities = []string{"configuration.replace", "runtime.reload"}
	plan, err = stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	binding.artifactDigest = testDigest("other-artifact")
	_, err = New(j, plan, journal.ApprovalInput{ID: "approval-artifact", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: binding})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("artifact drift error=%v", err)
	}
	binding.artifactDigest = testDigest("artifact")
	binding.configurationDigest = testDigest("other-configuration")
	_, err = New(j, plan, journal.ApprovalInput{ID: "approval-configuration", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{}}, Bindings{Caddy: binding})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("configuration drift error=%v", err)
	}
	binding.configurationDigest = testDigest("configuration")

	binding.secretSlots = []string{"token"}
	binding.secretDigests = map[string]string{"token": testDigest("secret-a")}
	manifest.Components[0].Capabilities = []string{"configuration.replace", "runtime.reload"}
	manifest.Components[0].SecretSlots = []string{"token"}
	plan, err = stack.BuildPlan(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(j, plan, journal.ApprovalInput{ID: "approval-b", ActorID: "operator-a", AuthorityDigest: testDigest("authority"), ExpiresAt: time.Now().Add(time.Hour), SecretRevisions: []journal.SecretRevision{{ComponentID: "proxy", Slot: "token", RevisionDigest: testDigest("secret-b")}}}, Bindings{Caddy: binding})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("secret drift error=%v", err)
	}
}

func testDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}

func TestImageArtifactDigest(t *testing.T) {
	want := testDigest("image")
	if got := imageArtifactDigest("registry.test/product@" + want); got != want {
		t.Fatalf("digest=%q want=%q", got, want)
	}
	for _, image := range []string{"", "registry.test/product:latest", "registry.test/product@sha256:bad"} {
		if got := imageArtifactDigest(image); got != "" {
			t.Fatalf("image=%q digest=%q", image, got)
		}
	}
}
