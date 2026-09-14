package authentik

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeContainer struct {
	request commandRequest
	running bool
	id      string
}
type fakeRunner struct {
	mu         sync.Mutex
	containers map[string]fakeContainer
	fail       commandKind
}

func (runner *fakeRunner) Run(_ context.Context, _ string, request commandRequest, stdout, _ io.Writer) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.fail == request.kind {
		return ErrEngine
	}
	switch request.kind {
	case commandVersion:
		_, _ = io.WriteString(stdout, `{"Version":"29.7.1","ApiVersion":"1.55","Os":"linux","Arch":"amd64"}`)
	case commandImageInspect:
		record := imageInspect{ID: imageDigest(request.spec.Specification.Image), RepoDigests: []string{request.spec.Specification.Image}}
		record.Config.User = "1000"
		record.Config.Entrypoint = []string{"dumb-init", "--", "ak"}
		record.Config.Labels = map[string]string{"org.opencontainers.image.version": "refs/tags/version/2026.5.2"}
		value, _ := json.Marshal([]imageInspect{record})
		_, _ = stdout.Write(value)
	case commandContainerList:
		if _, ok := runner.containers[request.name]; ok {
			_, _ = io.WriteString(stdout, request.name+"\n")
		}
	case commandContainerInspect:
		container, ok := runner.containers[request.name]
		if !ok {
			return ErrEngine
		}
		value, _ := json.Marshal([]containerInspect{fakeInspect(request.name, container)})
		_, _ = stdout.Write(value)
	case commandCreate:
		if _, ok := runner.containers[request.name]; ok {
			return ErrEngine
		}
		runner.containers[request.name] = fakeContainer{request: request, id: hex.EncodeToString(sha256.New().Sum([]byte(request.name)))[:64]}
	case commandStart:
		container, ok := runner.containers[request.name]
		if !ok {
			return ErrEngine
		}
		container.running = true
		runner.containers[request.name] = container
	case commandStop:
		container, ok := runner.containers[request.name]
		if !ok {
			return ErrEngine
		}
		container.running = false
		runner.containers[request.name] = container
	case commandRename:
		container, ok := runner.containers[request.name]
		if !ok {
			return ErrEngine
		}
		delete(runner.containers, request.name)
		runner.containers[request.destination] = container
	case commandRemove:
		if _, ok := runner.containers[request.name]; !ok {
			return ErrEngine
		}
		delete(runner.containers, request.name)
	case commandHealth:
		container, ok := runner.containers[request.name]
		if !ok || !container.running {
			return ErrEngine
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func fakeInspect(name string, container fakeContainer) containerInspect {
	request := container.request
	spec := request.spec
	role := request.role
	var record containerInspect
	record.ID = container.id
	record.Name = "/" + name
	record.Image = spec.ImageID
	record.Config.Image = spec.Specification.Image
	record.Config.User = strconv.FormatUint(uint64(spec.Specification.UID), 10) + ":" + strconv.FormatUint(uint64(spec.Specification.GID), 10)
	record.Config.Entrypoint = []string{"dumb-init", "--", "ak"}
	record.Config.Cmd = []string{string(role)}
	record.Config.Labels = map[string]string{labelAdapter: adapterLabelValue, labelComponent: spec.ComponentID, labelConfiguration: spec.ConfigurationDigest, labelDatabaseSecret: spec.DatabaseSecretRevision, labelKeySecret: spec.KeySecretRevision, labelOperation: spec.OperationID, labelRole: string(role)}
	metrics := spec.Specification.ServerMetricsPort
	httpPort := spec.Specification.HTTPPort
	httpsPort := spec.Specification.HTTPSPort
	if role == RoleWorker {
		metrics = spec.Specification.WorkerMetricsPort
		httpPort = spec.Specification.WorkerHTTPPort
		httpsPort = spec.Specification.WorkerHTTPSPort
	}
	record.Config.Env = []string{"PATH=/usr/bin:/bin", "AUTHENTIK_POSTGRESQL__HOST=" + spec.Specification.DatabaseHost, "AUTHENTIK_POSTGRESQL__PORT=" + strconv.Itoa(int(spec.Specification.DatabasePort)), "AUTHENTIK_POSTGRESQL__NAME=" + spec.Specification.Database, "AUTHENTIK_POSTGRESQL__USER=" + spec.Specification.DatabaseRole, "AUTHENTIK_POSTGRESQL__PASSWORD=file://" + databaseSecretTarget, "AUTHENTIK_POSTGRESQL__SSLMODE=disable", "AUTHENTIK_SECRET_KEY=file://" + keySecretTarget, "AUTHENTIK_ERROR_REPORTING__ENABLED=false", "AUTHENTIK_DISABLE_UPDATE_CHECK=true", "AUTHENTIK_LISTEN__HTTP=127.0.0.1:" + strconv.Itoa(int(httpPort)), "AUTHENTIK_LISTEN__HTTPS=127.0.0.1:" + strconv.Itoa(int(httpsPort)), "AUTHENTIK_LISTEN__METRICS=127.0.0.1:" + strconv.Itoa(int(metrics))}
	record.HostConfig.ReadonlyRootfs = true
	record.HostConfig.CapDrop = []string{"ALL"}
	record.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	record.HostConfig.NetworkMode = "host"
	record.HostConfig.ShmSize = 536870912
	record.HostConfig.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=67108864"}
	record.Mounts = append(record.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{"bind", request.dataPath, dataTarget, true}, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{"bind", request.templatesPath, templatesTarget, false}, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{"bind", request.databaseSecretPath, databaseSecretTarget, false}, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{"bind", request.keySecretPath, keySecretTarget, false})
	if role == RoleWorker {
		record.Mounts = append(record.Mounts, struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		}{"bind", request.certsPath, certsTarget, true})
	}
	record.State.Running = container.running
	return record
}

type fixture struct {
	adapter                    *Adapter
	runner                     *fakeRunner
	root, state, data, secrets string
	spec                       Specification
	db, key                    []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	data := filepath.Join(root, "data")
	secrets := filepath.Join(root, "secrets")
	for _, path := range []string{state, data, secrets} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	component := filepath.Join(data, "identity")
	if err := os.Mkdir(component, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"data", "certs", "templates"} {
		if err := os.Mkdir(filepath.Join(component, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	db := []byte("database-password-value")
	key := []byte("authentik-secret-key-value-with-more-than-fifty-characters-1234567890")
	if err := os.WriteFile(filepath.Join(secrets, "identity-postgresql-password"), db, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secrets, "identity-secret-key"), key, 0o400); err != nil {
		t.Fatal(err)
	}
	binary := "/usr/bin/true"
	binaryValue, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := Open(Options{DockerBinary: binary, DockerBinaryDigest: digest(binaryValue), StateRoot: state, DataRoot: data, SecretRoot: secrets, ContainerPrefix: "gotth", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{containers: map[string]fakeContainer{}}
	adapter.runner = runner
	spec := Specification{Image: "ghcr.io/goauthentik/server@sha256:" + string(bytes.Repeat([]byte{'1'}, 64)), DatabaseHost: "127.0.0.1", DatabasePort: 55432, Database: "authentik", DatabaseRole: "authentik", HTTPPort: 19000, HTTPSPort: 19443, WorkerHTTPPort: 19001, WorkerHTTPSPort: 19444, ServerMetricsPort: 19300, WorkerMetricsPort: 19301, UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	t.Cleanup(func() { _ = adapter.Close() })
	return &fixture{adapter: adapter, runner: runner, root: root, state: state, data: data, secrets: secrets, spec: spec, db: db, key: key}
}

func (f *fixture) request(operation string, spec Specification) Request {
	return Request{OperationID: operation, ComponentID: "identity", Specification: spec, ConfigurationDigest: mustConfigDigest(spec), DatabaseSecretRevisionDigest: digest(f.db), KeySecretRevisionDigest: digest(f.key)}
}
func mustConfigDigest(spec Specification) string {
	value, err := configurationDigest(spec)
	if err != nil {
		panic(err)
	}
	return value
}

func TestFreshInstallAndRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := f.adapter.CreateCandidate(ctx, "install-one", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := f.adapter.StartCandidate(ctx, "install-one", RoleServer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.VerifyRole(ctx, "install-one", RoleServer, DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.StartCandidate(ctx, "install-one", RoleWorker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Verify(ctx, "install-one", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.StopCandidate(ctx, "install-one", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.RemoveCandidate(ctx, "install-one", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := f.adapter.Verify(ctx, "install-one", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
}

func TestManagedReplacementReopenAndRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	installPair(t, f, "install-one", f.spec)
	replacement := f.spec
	replacement.Image = "ghcr.io/goauthentik/server@sha256:" + string(bytes.Repeat([]byte{'2'}, 64))
	prepared, summary, err := f.adapter.Preflight(ctx, f.request("replace-two", replacement))
	if err != nil {
		t.Fatal(err)
	}
	if !summary.HadPrevious || !summary.PreviousServerRunning || !summary.PreviousWorkerRunning {
		t.Fatal(summary)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.StopPrevious(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.PreservePrevious(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
		if _, err := f.adapter.PreservePrevious(ctx, "replace-two", role); err != nil {
			t.Fatalf("idempotent preserve %s: %v", role, err)
		}
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := f.adapter.CreateCandidate(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := f.adapter.StartCandidate(ctx, "replace-two", RoleServer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.StartCandidate(ctx, "replace-two", RoleWorker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Verify(ctx, "replace-two", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.StopCandidate(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := f.adapter.RemoveCandidate(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := f.adapter.RestorePrevious(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
		if _, err := f.adapter.RestorePrevious(ctx, "replace-two", role); err != nil {
			t.Fatalf("idempotent restore %s: %v", role, err)
		}
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := f.adapter.RestartPrevious(ctx, "replace-two", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := f.adapter.Verify(ctx, "replace-two", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
}

func installPair(t *testing.T, f *fixture, operation string, spec Specification) {
	t.Helper()
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request(operation, spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := f.adapter.CreateCandidate(ctx, operation, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.adapter.StartCandidate(ctx, operation, RoleServer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.StartCandidate(ctx, operation, RoleWorker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Verify(ctx, operation, DesiredCandidate); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsOrderSecretAndUnknownOutcome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.CreateCandidate(ctx, "install-one", RoleWorker); err != ErrRecoveryRequired {
		t.Fatalf("worker-first create: %v", err)
	}
	if err := os.Chmod(filepath.Join(f.secrets, "identity-secret-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.secrets, "identity-secret-key"), []byte("changed"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.CreateCandidate(ctx, "install-one", RoleServer); err != ErrConflict {
		t.Fatalf("secret drift: %v", err)
	}
}

func TestPersistentDirectoryGrowthDoesNotInvalidateBinding(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.data, "identity", "data", "runtime-created"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.CreateCandidate(ctx, "install-one", RoleServer); err != nil {
		t.Fatalf("directory contents changed a stable directory binding: %v", err)
	}
}

func TestCorruptJournalErrorIsNotHiddenAsAbsentOperation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.state, transactionsName, transactionName("install-one"))
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Verify(ctx, "install-one", DesiredCandidate); err != ErrUnsafeState {
		t.Fatalf("corrupt journal error was hidden: %v", err)
	}
}

func TestPreflightRejectsWorkerRunningWithoutServer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	installPair(t, f, "install-one", f.spec)
	serverName := f.adapter.containerName("identity", RoleServer)
	f.runner.mu.Lock()
	server := f.runner.containers[serverName]
	server.running = false
	f.runner.containers[serverName] = server
	f.runner.mu.Unlock()
	replacement := f.spec
	replacement.ServerMetricsPort = 19302
	if _, _, err := f.adapter.Preflight(ctx, f.request("replace-two", replacement)); err != ErrRecoveryRequired {
		t.Fatalf("worker-only running state accepted: %v", err)
	}
}

func TestObserveAndReconcileInterruptedStage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	transactions := filepath.Join(f.state, transactionsName)
	if err := os.Rename(filepath.Join(transactions, transactionName("install-one")), filepath.Join(transactions, stageName("install-one"))); err != nil {
		t.Fatal(err)
	}
	observation, err := f.adapter.Observe(ctx, "install-one")
	if err != nil {
		t.Fatal(err)
	}
	if observation.StageState != StateIncomplete {
		t.Fatalf("stage state = %s", observation.StageState)
	}
	_, state, err := f.adapter.ReconcileStage("install-one")
	if err != nil || state != StateCandidate {
		t.Fatalf("reconcile = %s, %v", state, err)
	}
	observation, err = f.adapter.Observe(ctx, "install-one")
	if err != nil || observation.StageState != StateCandidate || observation.Server.PrimaryState != StateAbsent || observation.Worker.PrimaryState != StateAbsent {
		t.Fatalf("observation = %#v, %v", observation, err)
	}
}

func TestRollbackStageBeforeMutationIsDurableAndIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepared, _, err := f.adapter.Preflight(ctx, f.request("install-one", f.spec))
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.adapter.Stage(prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.adapter.Stage(prepared)
	if err != nil || first != second {
		t.Fatalf("idempotent stage = %#v, %v", second, err)
	}
	if err := f.adapter.RollbackStage(ctx, "install-one"); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.RollbackStage(ctx, "install-one"); err != nil {
		t.Fatal(err)
	}
	_, state, err := f.adapter.ReconcileStage("install-one")
	if err != nil || state != StateAbsent {
		t.Fatalf("rolled-back stage = %s, %v", state, err)
	}
}

func TestPreflightRejectsShortKeyAndSymlinkedComponentRoot(t *testing.T) {
	t.Run("short key", func(t *testing.T) {
		f := newFixture(t)
		short := bytes.Repeat([]byte{'k'}, 49)
		path := filepath.Join(f.secrets, "identity-secret-key")
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, short, 0o400); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		request := f.request("install-one", f.spec)
		request.KeySecretRevisionDigest = digest(short)
		if _, _, err := f.adapter.Preflight(context.Background(), request); err != ErrSecret {
			t.Fatalf("short key: %v", err)
		}
	})
	t.Run("component symlink", func(t *testing.T) {
		f := newFixture(t)
		component := filepath.Join(f.data, "identity")
		outside := filepath.Join(f.root, "outside")
		if err := os.Rename(component, outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, component); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.adapter.Preflight(context.Background(), f.request("install-one", f.spec)); err != ErrUnsafeState {
			t.Fatalf("component symlink: %v", err)
		}
	})
}
