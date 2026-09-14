package caddy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	oldConfig = ":8080 {\n\trespond old\n}\n"
	newConfig = ":8080 {\n\trespond new\n}\n"
)

type fakeRuntime struct {
	mu    sync.Mutex
	value []byte
	err   error
}

func (runtime *fakeRuntime) Read(ctx context.Context) ([]byte, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runtime.err != nil {
		return nil, runtime.err
	}
	return append([]byte(nil), runtime.value...), nil
}

func (runtime *fakeRuntime) set(value []byte) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.value = append([]byte(nil), value...)
}

type fakeRunner struct {
	runtime      *fakeRuntime
	fail         string
	ignoreReload bool
	output       []byte
	mu           sync.Mutex
	calls        [][]string
	envs         [][]string
}

func (runner *fakeRunner) Run(ctx context.Context, _ string, request commandRequest, stdout, _ io.Writer) error {
	arguments, err := fixedArguments(request)
	if err != nil {
		return err
	}
	runner.mu.Lock()
	runner.calls = append(runner.calls, append([]string(nil), arguments...))
	runner.envs = append(runner.envs, append([]string(nil), request.environment...))
	runner.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(arguments) == 0 {
		return errors.New("missing command")
	}
	if runner.fail == arguments[0] {
		return errors.New("injected command failure")
	}
	switch arguments[0] {
	case "adapt":
		value, err := os.ReadFile(request.configPath)
		if err != nil {
			return err
		}
		adapted := fakeAdapt(value)
		if runner.output != nil {
			adapted = runner.output
		}
		_, err = stdout.Write(adapted)
		return err
	case "validate":
		return nil
	case "reload":
		value, err := os.ReadFile(request.configPath)
		if err != nil {
			return err
		}
		if !runner.ignoreReload {
			runner.runtime.set(fakeAdapt(value))
		}
		return nil
	default:
		return errors.New("unexpected command")
	}
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}

func fakeAdapt(config []byte) []byte {
	body := "old"
	admin := "127.0.0.1:2019"
	if bytes.Contains(config, []byte("new")) {
		body = "new"
	} else if bytes.Contains(config, []byte("other")) {
		body = "other"
	}
	if bytes.Contains(config, []byte("admin-other")) {
		admin = "127.0.0.1:2020"
	}
	return []byte(`{"admin":{"listen":"` + admin + `"},"apps":{"http":{"servers":{"main":{"routes":[{"handle":[{"body":"` + body + `","handler":"static_response"}]}]}}}}}`)
}

type fixture struct {
	options Options
	adapter *Adapter
	runtime *fakeRuntime
	runner  *fakeRunner
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	configDir := filepath.Join(root, "config")
	stateDir := filepath.Join(root, "state")
	for _, path := range []string{binDir, configDir, stateDir} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(binDir, "caddy")
	if err := os.WriteFile(binary, []byte("fake executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configDir, "Caddyfile")
	if err := os.WriteFile(config, []byte(oldConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{CaddyBinary: binary, CaddyBinaryDigest: digest([]byte("fake executable")), ConfigPath: config, StateRoot: stateDir, WorkingDirectory: root, AdminURL: "http://127.0.0.1:2019", Timeout: time.Second}
	adapter, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{value: fakeAdapt([]byte(oldConfig))}
	runner := &fakeRunner{runtime: runtime}
	adapter.runtime = runtime
	adapter.runner = runner
	t.Cleanup(func() {
		if !adapter.closed {
			_ = adapter.Close()
		}
	})
	return &fixture{options: options, adapter: adapter, runtime: runtime, runner: runner}
}

func prepare(t *testing.T, fixture *fixture, operationID string, config string) (*Prepared, Summary) {
	t.Helper()
	value := []byte(config)
	prepared, result, err := fixture.adapter.Preflight(context.Background(), Request{
		OperationID: operationID, ComponentID: "caddy-main", Configuration: value, ExpectedDigest: digest(value),
	})
	if err != nil {
		t.Fatal(err)
	}
	return prepared, result
}

func TestLifecycleAndReopen(t *testing.T) {
	fixture := newFixture(t)
	prepared, expected := prepare(t, fixture, "operation-one", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if result, state, err := fixture.adapter.ReconcileStage("operation-one"); err != nil || state != StatePresent || result != expected {
		t.Fatalf("reconcile = %#v, %q, %v", result, state, err)
	}
	observation, err := fixture.adapter.Observe(context.Background(), "operation-one")
	if err != nil || observation.StageState != StatePresent || observation.FileState != StatePrevious || observation.RuntimeState != StatePrevious {
		t.Fatalf("initial observation = %#v, %v", observation, err)
	}
	if _, err := fixture.adapter.Install("operation-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatalf("duplicate stage after install: %v", err)
	}
	observation, err = fixture.adapter.Observe(context.Background(), "operation-one")
	if err != nil || observation.FileState != StateCandidate || observation.RuntimeState != StatePrevious {
		t.Fatalf("installed observation = %#v, %v", observation, err)
	}
	if _, err := fixture.adapter.Activate(context.Background(), "operation-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-one", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	fixture.adapter = reopened
	reopened.runtime = fixture.runtime
	reopened.runner = fixture.runner
	if _, err := reopened.Verify(context.Background(), "operation-one", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Restore("operation-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Reactivate(context.Background(), "operation-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Verify(context.Background(), "operation-one", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(fixture.options.ConfigPath)
	if err != nil || string(value) != oldConfig {
		t.Fatalf("restored config = %q, %v", value, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightRejectsInvalidInputWithoutDisclosure(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		component string
		config    []byte
		digest    string
		want      error
	}{
		{name: "operation", operation: "../escape", component: "caddy-main", config: []byte(newConfig), digest: digest([]byte(newConfig)), want: ErrInvalidInput},
		{name: "component", operation: "operation-two", component: "UPPER", config: []byte(newConfig), digest: digest([]byte(newConfig)), want: ErrInvalidInput},
		{name: "empty", operation: "operation-two", component: "caddy-main", config: nil, digest: digest(nil), want: ErrInvalidInput},
		{name: "large", operation: "operation-two", component: "caddy-main", config: bytes.Repeat([]byte{'x'}, MaxConfigBytes+1), digest: digest(bytes.Repeat([]byte{'x'}, MaxConfigBytes+1)), want: ErrLimit},
		{name: "utf8", operation: "operation-two", component: "caddy-main", config: []byte{0xff}, digest: digest([]byte{0xff}), want: ErrInvalidInput},
		{name: "nul", operation: "operation-two", component: "caddy-main", config: []byte("x\x00y"), digest: digest([]byte("x\x00y")), want: ErrInvalidInput},
		{name: "import", operation: "operation-two", component: "caddy-main", config: []byte("import secret\n"), digest: digest([]byte("import secret\n")), want: ErrInvalidInput},
		{name: "environment-short", operation: "operation-two", component: "caddy-main", config: []byte(":80 { respond {$TOKEN} }\n"), digest: digest([]byte(":80 { respond {$TOKEN} }\n")), want: ErrInvalidInput},
		{name: "environment-long", operation: "operation-two", component: "caddy-main", config: []byte(":80 { respond {env.TOKEN} }\n"), digest: digest([]byte(":80 { respond {env.TOKEN} }\n")), want: ErrInvalidInput},
		{name: "digest-shape", operation: "operation-two", component: "caddy-main", config: []byte(newConfig), digest: "sha256:no", want: ErrInvalidInput},
		{name: "digest-mismatch", operation: "operation-two", component: "caddy-main", config: []byte(newConfig), digest: digest([]byte(oldConfig)), want: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: test.operation, ComponentID: test.component, Configuration: test.config, ExpectedDigest: test.digest})
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if err != nil && (strings.Contains(err.Error(), "TOKEN") || strings.Contains(err.Error(), "escape")) {
				t.Fatalf("error disclosed input: %v", err)
			}
		})
	}
	fixture := newFixture(t)
	_, _, err := fixture.adapter.Preflight(nil, Request{OperationID: "nil-context", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil context got %v", err)
	}
}

func TestPreflightCommandAndRuntimeFailures(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runner.fail = "validate"
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("adapt", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runner.fail = "adapt"
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("runtime drift", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runtime.set(fakeAdapt([]byte("other")))
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("runtime unavailable", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runtime.err = ErrRuntime
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrRuntime) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("candidate moves admin endpoint", func(t *testing.T) {
		fixture := newFixture(t)
		config := []byte(newConfig + "# admin-other\n")
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: config, ExpectedDigest: digest(config)})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("invalid adapted JSON", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runner.output = []byte("[]")
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("oversized adapted JSON", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.runner.output = bytes.Repeat([]byte{'x'}, MaxJSONBytes+1)
		_, _, err := fixture.adapter.Preflight(context.Background(), Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		fixture := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := fixture.adapter.Preflight(ctx, Request{OperationID: "operation-three", ComponentID: "caddy-main", Configuration: []byte(newConfig), ExpectedDigest: digest([]byte(newConfig))})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestStageConflictAndRecovery(t *testing.T) {
	t.Run("conflicting id", func(t *testing.T) {
		fixture := newFixture(t)
		first, _ := prepare(t, fixture, "operation-four", newConfig)
		secondConfig := ":8080 {\n\trespond newer\n}\n"
		second, _ := prepare(t, fixture, "operation-four", secondConfig)
		if _, err := fixture.adapter.Stage(first); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Stage(second); !errors.Is(err, ErrConflict) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("complete temporary", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, expected := prepare(t, fixture, "operation-five", newConfig)
		defaults := fixture.adapter.ops
		fixture.adapter.ops.rename = func(*os.Root, string, string) error { return errors.New("injected") }
		if _, err := fixture.adapter.Stage(prepared); !errors.Is(err, ErrStorage) {
			t.Fatalf("got %v", err)
		}
		fixture.adapter.ops = defaults
		result, state, err := fixture.adapter.ReconcileStage("operation-five")
		if err != nil || state != StatePresent || result != expected {
			t.Fatalf("reconcile = %#v, %q, %v", result, state, err)
		}
	})
	t.Run("partial temporary", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-six", newConfig)
		fixture.adapter.ops.write = func(*os.File, []byte) error { return errors.New("injected") }
		if _, err := fixture.adapter.Stage(prepared); !errors.Is(err, ErrStorage) {
			t.Fatalf("got %v", err)
		}
		_, state, err := fixture.adapter.ReconcileStage("operation-six")
		if state != StateIncomplete || !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("reconcile = %q, %v", state, err)
		}
		fixture.adapter.ops = defaultStorageOps()
		if err := fixture.adapter.RollbackStage(context.Background(), "operation-six"); err != nil {
			t.Fatal(err)
		}
		if _, state, err := fixture.adapter.ReconcileStage("operation-six"); err != nil || state != StateAbsent {
			t.Fatalf("after rollback = %q, %v", state, err)
		}
	})
	t.Run("committed baseline", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-stage-rollback", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if err := fixture.adapter.RollbackStage(context.Background(), "operation-stage-rollback"); err != nil {
			t.Fatal(err)
		}
		if _, state, err := fixture.adapter.ReconcileStage("operation-stage-rollback"); err != nil || state != StateAbsent {
			t.Fatalf("after rollback = %q, %v", state, err)
		}
	})
	t.Run("refuses after install", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-stage-live", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Install("operation-stage-live"); err != nil {
			t.Fatal(err)
		}
		if err := fixture.adapter.RollbackStage(context.Background(), "operation-stage-live"); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestUnexpectedStatesRefuseMutation(t *testing.T) {
	fixture := newFixture(t)
	prepared, _ := prepare(t, fixture, "operation-seven", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.options.ConfigPath, []byte(":8080 {\n respond other\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Install("operation-seven"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("install got %v", err)
	}
	if _, err := fixture.adapter.Restore("operation-seven"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("restore got %v", err)
	}
	if err := os.WriteFile(fixture.options.ConfigPath, []byte(newConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.runtime.set(fakeAdapt([]byte("other")))
	if _, err := fixture.adapter.Activate(context.Background(), "operation-seven"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("activate got %v", err)
	}
}

func TestActivationFailureAndIdempotency(t *testing.T) {
	t.Run("command failure", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-activate", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Install("operation-activate"); err != nil {
			t.Fatal(err)
		}
		fixture.runner.fail = "reload"
		if _, err := fixture.adapter.Activate(context.Background(), "operation-activate"); !errors.Is(err, ErrCommand) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-idempotent", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Install("operation-idempotent"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Activate(context.Background(), "operation-idempotent"); err != nil {
			t.Fatal(err)
		}
		fixture.runner.mu.Lock()
		calls := len(fixture.runner.calls)
		fixture.runner.mu.Unlock()
		if _, err := fixture.adapter.Activate(context.Background(), "operation-idempotent"); err != nil {
			t.Fatal(err)
		}
		fixture.runner.mu.Lock()
		defer fixture.runner.mu.Unlock()
		if len(fixture.runner.calls) != calls {
			t.Fatalf("idempotent activation ran command: %d -> %d", calls, len(fixture.runner.calls))
		}
	})
	t.Run("post reload mismatch", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-mismatch", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Install("operation-mismatch"); err != nil {
			t.Fatal(err)
		}
		fixture.runner.ignoreReload = true
		if _, err := fixture.adapter.Activate(context.Background(), "operation-mismatch"); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("wrong verification state", func(t *testing.T) {
		fixture := newFixture(t)
		prepared, _ := prepare(t, fixture, "operation-verify", newConfig)
		if _, err := fixture.adapter.Stage(prepared); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.adapter.Verify(context.Background(), "operation-verify", DesiredCandidate); !errors.Is(err, ErrConflict) {
			t.Fatalf("got %v", err)
		}
		if _, err := fixture.adapter.Verify(context.Background(), "operation-verify", DesiredState("bad")); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid desired got %v", err)
		}
	})
}

func TestUnstagedAndClosedOperations(t *testing.T) {
	fixture := newFixture(t)
	observation, err := fixture.adapter.Observe(context.Background(), "not-staged")
	if err != nil || observation.StageState != StateAbsent || observation.FileState != StateAbsent || observation.RuntimeState != StateAbsent {
		t.Fatalf("observation = %#v, %v", observation, err)
	}
	if _, state, err := fixture.adapter.ReconcileStage("not-staged"); err != nil || state != StateAbsent {
		t.Fatalf("reconcile = %q, %v", state, err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "not-staged"); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(fixture.options.StateRoot, transactionDirName, temporaryStageName("incomplete-stage"))
	if err := os.Mkdir(temporary, 0o700); err != nil {
		t.Fatal(err)
	}
	observation, err = fixture.adapter.Observe(context.Background(), "incomplete-stage")
	if err != nil || observation.StageState != StateIncomplete {
		t.Fatalf("incomplete = %#v, %v", observation, err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Observe(context.Background(), "not-staged"); !errors.Is(err, ErrClosed) {
		t.Fatalf("observe closed got %v", err)
	}
	if _, err := fixture.adapter.Install("not-staged"); !errors.Is(err, ErrClosed) {
		t.Fatalf("install closed got %v", err)
	}
	if _, err := fixture.adapter.Restore("not-staged"); !errors.Is(err, ErrClosed) {
		t.Fatalf("restore closed got %v", err)
	}
	if _, err := fixture.adapter.Activate(context.Background(), "not-staged"); !errors.Is(err, ErrClosed) {
		t.Fatalf("activate closed got %v", err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "not-staged"); !errors.Is(err, ErrClosed) {
		t.Fatalf("rollback stage closed got %v", err)
	}
}

func TestPreparedValueIsAdapterLocal(t *testing.T) {
	first := newFixture(t)
	second := newFixture(t)
	prepared, _ := prepare(t, first, "local-prepared", newConfig)
	if _, err := second.adapter.Stage(prepared); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross adapter got %v", err)
	}
	if _, err := first.adapter.Stage(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil got %v", err)
	}
}

func TestFixedCommandArgumentsAndEnvironment(t *testing.T) {
	fixture := newFixture(t)
	prepared, _ := prepare(t, fixture, "operation-eight", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Install("operation-eight"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Activate(context.Background(), "operation-eight"); err != nil {
		t.Fatal(err)
	}
	fixture.runner.mu.Lock()
	defer fixture.runner.mu.Unlock()
	verbs := make([]string, 0, len(fixture.runner.calls))
	for _, call := range fixture.runner.calls {
		verbs = append(verbs, call[0])
	}
	if !reflect.DeepEqual(verbs, []string{"adapt", "adapt", "validate", "reload"}) {
		t.Fatalf("verbs = %#v", verbs)
	}
	last := fixture.runner.calls[len(fixture.runner.calls)-1]
	if argumentValue(last, "--adapter") != "caddyfile" || argumentValue(last, "--address") != "127.0.0.1:2019" {
		t.Fatalf("reload arguments = %#v", last)
	}
	for _, environment := range fixture.runner.envs {
		if len(environment) != 4 {
			t.Fatalf("environment = %#v", environment)
		}
		for _, item := range environment {
			if strings.HasPrefix(item, "HTTP_PROXY=") || strings.HasPrefix(item, "TOKEN=") {
				t.Fatalf("ambient environment leaked: %#v", environment)
			}
		}
	}
}
