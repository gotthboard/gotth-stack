package postgresql

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	testImageA = "postgres@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testImageB = "postgres@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeDocker struct {
	mu         sync.Mutex
	containers map[string]*containerRecord
	dataRoot   string
	fail       commandKind
	calls      []commandRequest
	envs       [][]string
}

func (runner *fakeDocker) Run(ctx context.Context, _ string, request commandRequest, stdout, _ io.Writer) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	runner.calls = append(runner.calls, request)
	runner.envs = append(runner.envs, append([]string(nil), request.environment...))
	if runner.fail == request.kind {
		return errors.New("injected failure")
	}
	switch request.kind {
	case commandVersion:
		return encodeFake(stdout, map[string]any{"Version": "29.7.1", "ApiVersion": "1.55", "Os": "linux", "Arch": "amd64"})
	case commandImageInspect:
		id := imageDigest(request.spec.Specification.Image)
		return encodeFake(stdout, []imageInspect{{
			ID: id, RepoDigests: []string{request.spec.Specification.Image},
			Config: struct {
				Env        []string `json:"Env"`
				Entrypoint []string `json:"Entrypoint"`
				Cmd        []string `json:"Cmd"`
			}{Env: []string{"PG_MAJOR=17"}, Entrypoint: []string{"docker-entrypoint.sh"}, Cmd: []string{"postgres"}},
		}})
	case commandContainerList:
		if runner.containers[request.name] != nil {
			_, err := io.WriteString(stdout, request.name+"\n")
			return err
		}
		return nil
	case commandContainerInspect:
		record := runner.containers[request.name]
		if record == nil {
			return errors.New("not found")
		}
		return encodeFake(stdout, []containerInspect{fakeContainerInspect(request.name, record, runner.dataRoot)})
	case commandStop:
		record := runner.containers[request.name]
		if record == nil {
			return errors.New("not found")
		}
		record.running = false
		return nil
	case commandRename:
		record := runner.containers[request.name]
		if record == nil || runner.containers[request.destination] != nil {
			return errors.New("rename conflict")
		}
		delete(runner.containers, request.name)
		runner.containers[request.destination] = record
		return nil
	case commandCreate:
		if runner.containers[request.name] != nil {
			return errors.New("already exists")
		}
		runner.containers[request.name] = &containerRecord{spec: request.spec, id: fakeContainerID(request.name)}
		return nil
	case commandStart:
		record := runner.containers[request.name]
		if record == nil {
			return errors.New("not found")
		}
		record.running = true
		version := filepath.Join(runner.dataRoot, record.spec.DataName, "PG_VERSION")
		if _, err := os.Stat(version); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(version, []byte("17\n"), 0o600); err != nil {
				return err
			}
		}
		return nil
	case commandRemove:
		record := runner.containers[request.name]
		if record == nil || record.running {
			return errors.New("cannot remove")
		}
		delete(runner.containers, request.name)
		return nil
	case commandReady:
		if record := runner.containers[request.name]; record == nil || !record.running {
			return errors.New("not ready")
		}
		return nil
	case commandIdentity:
		record := runner.containers[request.name]
		if record == nil || !record.running {
			return errors.New("not running")
		}
		_, err := io.WriteString(stdout, "170010|"+record.spec.Specification.Database+"|"+record.spec.Specification.Role+"|f\n")
		return err
	default:
		return errors.New("unexpected command")
	}
}

func encodeFake(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}

func fakeContainerID(name string) string {
	return digest([]byte(name))[len("sha256:"):]
}

func fakeContainerInspect(name string, record *containerRecord, dataRoot string) containerInspect {
	spec := record.spec
	result := containerInspect{ID: record.id, Name: "/" + name, Image: spec.ImageID}
	result.Config.Image = spec.Specification.Image
	result.Config.User = uintString(spec.Specification.UID) + ":" + uintString(spec.Specification.GID)
	result.Config.Env = []string{
		"POSTGRES_DB=" + spec.Specification.Database,
		"POSTGRES_USER=" + spec.Specification.Role,
		"POSTGRES_PASSWORD_FILE=" + secretTarget,
	}
	result.Config.Labels = map[string]string{
		labelAdapter: adapterLabelValue, labelComponent: spec.ComponentID,
		labelConfiguration: spec.ConfigurationDigest, labelSecretRevision: spec.SecretRevision,
		labelOperation: spec.OperationID,
	}
	result.Config.Entrypoint = []string{"docker-entrypoint.sh"}
	result.Config.Cmd = []string{"postgres"}
	result.HostConfig.ReadonlyRootfs = true
	result.HostConfig.CapDrop = []string{"ALL"}
	result.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	result.HostConfig.Tmpfs = map[string]string{
		"/tmp":                "rw,noexec,nosuid,nodev,size=67108864",
		"/var/run/postgresql": "rw,noexec,nosuid,nodev,size=16777216",
	}
	result.HostConfig.PortBindings = map[string][]portMap{
		"5432/tcp": {{HostIP: "127.0.0.1", HostPort: uintString(uint32(spec.Specification.Port))}},
	}
	result.Mounts = []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{
		{Type: "bind", Source: filepath.Join(dataRoot, spec.DataName), Destination: dataTarget, RW: true},
		{Type: "bind", Source: filepath.Join(filepath.Dir(dataRoot), "secrets", spec.SecretName), Destination: secretTarget, RW: false},
	}
	result.State.Running = record.running
	return result
}

func uintString(value uint32) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [10]byte
	index := len(buffer)
	for value != 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

type fixture struct {
	options Options
	adapter *Adapter
	runner  *fakeDocker
	spec    Specification
	secret  []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	paths := map[string]string{
		"state": filepath.Join(root, "state"), "data": filepath.Join(root, "data"),
		"secrets": filepath.Join(root, "secrets"),
	}
	for _, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	binary := "/usr/bin/true"
	component := "postgres-main"
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	if uid == 0 {
		uid, gid = 999, 999
	}
	data := filepath.Join(paths["data"], component)
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(data, int(uid), int(gid)); err != nil {
			t.Fatal(err)
		}
	}
	secret := []byte("correct horse battery staple\n")
	secretPath := filepath.Join(paths["secrets"], component+"-password")
	if err := os.WriteFile(secretPath, secret, 0o400); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(secretPath, int(uid), int(gid)); err != nil {
			t.Fatal(err)
		}
	}
	options := Options{
		DockerBinary: binary, DockerBinaryDigest: digestFile(t, binary), StateRoot: paths["state"],
		DataRoot: paths["data"], SecretRoot: paths["secrets"], ContainerPrefix: "gotth", Timeout: time.Second,
	}
	adapter, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeDocker{containers: make(map[string]*containerRecord), dataRoot: paths["data"]}
	adapter.runner = runner
	fixture := &fixture{
		options: options, adapter: adapter, runner: runner, secret: secret,
		spec: Specification{Image: testImageA, Database: "gotth", Role: "gotth", Port: 15432, UID: uid, GID: gid},
	}
	t.Cleanup(func() {
		if !fixture.adapter.closed {
			_ = fixture.adapter.Close()
		}
	})
	return fixture
}

func (fixture *fixture) request(t *testing.T, operation string, spec Specification) Request {
	t.Helper()
	configuration, err := configurationDigest(spec)
	if err != nil {
		t.Fatal(err)
	}
	return Request{
		OperationID: operation, ComponentID: "postgres-main", Specification: spec,
		ConfigurationDigest: configuration, SecretRevisionDigest: digest(fixture.secret),
	}
}

func (fixture *fixture) preflightAndStage(t *testing.T, operation string, spec Specification) Summary {
	t.Helper()
	prepared, expected, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, operation, spec))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := fixture.adapter.Stage(prepared)
	if err != nil || actual != expected {
		t.Fatalf("stage = %#v, %v; want %#v", actual, err, expected)
	}
	return expected
}

func (fixture *fixture) reopen(t *testing.T) {
	t.Helper()
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, err := Open(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	adapter.runner = fixture.runner
	fixture.adapter = adapter
}

func TestFreshLifecycleReopenAndRollback(t *testing.T) {
	fixture := newFixture(t)
	expected := fixture.preflightAndStage(t, "operation-one", fixture.spec)
	if result, state, err := fixture.adapter.ReconcileStage("operation-one"); err != nil || state != StateCandidate || result != expected {
		t.Fatalf("reconcile = %#v, %q, %v", result, state, err)
	}
	observation, err := fixture.adapter.Observe(context.Background(), "operation-one")
	if err != nil || observation.PrimaryState != StateAbsent || observation.RollbackState != StateAbsent || observation.DataState != StatePrevious {
		t.Fatalf("initial observation = %#v, %v", observation, err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.PreservePrevious, fixture.adapter.CreateCandidate, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate, fixture.adapter.StartCandidate} {
		if _, err := action(context.Background(), "operation-one"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-one", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	observation, err = fixture.adapter.Observe(context.Background(), "operation-one")
	if err != nil || observation.PrimaryState != StateCandidate || observation.PrimaryPower != PowerRunning || observation.DataState != StateCandidate {
		t.Fatalf("candidate observation = %#v, %v", observation, err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	fixture.adapter = reopened
	reopened.runner = fixture.runner
	if _, err := reopened.Verify(context.Background(), "operation-one", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){reopened.StopCandidate, reopened.StopCandidate, reopened.RemoveCandidate, reopened.RemoveCandidate, reopened.RestorePrevious, reopened.RestartPrevious} {
		if _, err := action(context.Background(), "operation-one"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reopened.Verify(context.Background(), "operation-one", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	if value, err := os.ReadFile(filepath.Join(fixture.options.DataRoot, "postgres-main", "PG_VERSION")); err != nil || string(value) != "17\n" {
		t.Fatalf("preserved data = %q, %v", value, err)
	}
}

func TestManagedReplacementAndExactReverseRollback(t *testing.T) {
	fixture := newFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.options.DataRoot, "postgres-main", "PG_VERSION"), []byte("17\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldSpec := fixture.spec
	oldSpec.Image = testImageA
	oldRequest := fixture.request(t, "old-operation", oldSpec)
	oldConfig := oldRequest.ConfigurationDigest
	previous := managedSpec{
		OperationID: "old-operation", ComponentID: oldRequest.ComponentID, ConfigurationDigest: oldConfig,
		SecretRevision: oldRequest.SecretRevisionDigest, Specification: oldSpec, ImageID: imageDigest(oldSpec.Image),
		ContainerName: "gotth-postgres-main", DataName: "postgres-main", SecretName: "postgres-main-password",
	}
	fixture.runner.containers[previous.ContainerName] = &containerRecord{spec: previous, running: true, id: fakeContainerID("previous")}
	newSpec := fixture.spec
	newSpec.Image = testImageB
	expected := fixture.preflightAndStage(t, "operation-two", newSpec)
	if !expected.HadPrevious || !expected.PreviousRunning || expected.PreviousConfigDigest != oldConfig {
		t.Fatalf("summary = %#v", expected)
	}
	if _, err := fixture.adapter.StopPrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.StopPrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.PreservePrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.PreservePrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.StartCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.StartCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-two", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.StopCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.StopCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.RemoveCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.RemoveCandidate(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.RestorePrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.RestorePrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.RestartPrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if _, err := fixture.adapter.RestartPrevious(context.Background(), "operation-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-two", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	restored := fixture.runner.containers[previous.ContainerName]
	if restored == nil || !restored.running || !reflect.DeepEqual(restored.spec, previous) {
		t.Fatalf("restored = %#v", restored)
	}
}

func TestStageAndBindingsFailClosed(t *testing.T) {
	fixture := newFixture(t)
	prepared, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "operation-three", fixture.spec))
	if err != nil {
		t.Fatal(err)
	}
	foreign := newFixture(t)
	if _, err := foreign.adapter.Stage(prepared); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign stage = %v", err)
	}
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(fixture.options.SecretRoot, "postgres-main-password"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "operation-three"); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed secret binding = %v", err)
	}
}

func TestRollbackStageRefusesRuntimeMutation(t *testing.T) {
	fixture := newFixture(t)
	fixture.preflightAndStage(t, "operation-four", fixture.spec)
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "operation-four"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "operation-four"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("rollback after mutation = %v", err)
	}
}

func TestStageRecoveryConflictAndCleanRollback(t *testing.T) {
	fixture := newFixture(t)
	prepared, expected, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "operation-eight", fixture.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, state, err := fixture.adapter.ReconcileStage("missing-operation"); err != nil || state != StateAbsent {
		t.Fatalf("missing reconcile = %q, %v", state, err)
	}
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if actual, err := fixture.adapter.Stage(prepared); err != nil || actual != expected {
		t.Fatalf("duplicate stage = %#v, %v", actual, err)
	}
	transaction := filepath.Join(fixture.options.StateRoot, transactionsName, transactionName("operation-eight"))
	temporary := filepath.Join(fixture.options.StateRoot, transactionsName, stageName("operation-eight"))
	if err := os.Rename(transaction, temporary); err != nil {
		t.Fatal(err)
	}
	if actual, state, err := fixture.adapter.ReconcileStage("operation-eight"); err != nil || state != StateCandidate || actual != expected {
		t.Fatalf("stage recovery = %#v, %q, %v", actual, state, err)
	}
	changed := fixture.spec
	changed.Image = testImageB
	conflicting, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "operation-eight", changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Stage(conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting stage = %v", err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "operation-eight"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "operation-eight"); err != nil {
		t.Fatal(err)
	}
	if observation, err := fixture.adapter.Observe(context.Background(), "operation-eight"); err != nil || observation.StageState != StateAbsent {
		t.Fatalf("rolled-back observation = %#v, %v", observation, err)
	}
}

func TestStageTransactionModeIgnoresRestrictiveUmask(t *testing.T) {
	fixture := newFixture(t)
	prepared, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "operation-mode", fixture.spec))
	if err != nil {
		t.Fatal(err)
	}
	previous := syscall.Umask(0o777)
	defer syscall.Umask(previous)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(fixture.options.StateRoot, transactionsName, transactionName("operation-mode")))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("transaction mode = %04o", info.Mode().Perm())
	}
}

func TestMutationFailureAndInvalidCallsFailClosed(t *testing.T) {
	fixture := newFixture(t)
	fixture.preflightAndStage(t, "operation-nine", fixture.spec)
	fixture.runner.fail = commandCreate
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "operation-nine"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("create failure = %v", err)
	}
	fixture.runner.fail = 0
	if _, err := fixture.adapter.Verify(context.Background(), "operation-nine", DesiredState("garbage")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid desired state = %v", err)
	}
	if _, err := fixture.adapter.CreateCandidate(nil, "operation-nine"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil context = %v", err)
	}
	if _, err := fixture.adapter.CreateCandidate(context.Background(), "missing-operation"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("missing transaction = %v", err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "closed-preflight", fixture.spec)); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed preflight = %v", err)
	}
	if _, err := fixture.adapter.Observe(context.Background(), "operation-nine"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed observe = %v", err)
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-nine", DesiredCandidate); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed verify = %v", err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatalf("idempotent close = %v", err)
	}
}
