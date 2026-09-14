//go:build linux

package authentik

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const disposablePostgreSQLImage = "postgres@sha256:a426e44bac0b759c95894d68e1a0ac03ecc20b619f498a91aae373bf06d8508d"

func TestDisposableAuthentik2026_5(t *testing.T) {
	if os.Getenv("GOTTH_AUTHENTIK_IMAGE") == "" || os.Getenv("GOTTH_AUTHENTIK_DOCKER_BIN") == "" {
		t.Skip("set GOTTH_AUTHENTIK_IMAGE and GOTTH_AUTHENTIK_DOCKER_BIN")
	}
	if os.Geteuid() != 0 {
		t.Skip("disposable container proof requires root")
	}
	docker := os.Getenv("GOTTH_AUTHENTIK_DOCKER_BIN")
	image := os.Getenv("GOTTH_AUTHENTIK_IMAGE")
	root, err := os.MkdirTemp("", "gotth-authentik-proof.*")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "gotth-ak-proof-" + strconv.Itoa(os.Getpid())
	postgresName := prefix + "-postgres"
	cleanupNames := []string{postgresName}
	defer func() {
		for _, name := range cleanupNames {
			_ = dockerRun(docker, "rm", "-f", name)
		}
		_ = os.RemoveAll(root)
	}()
	postgresPort := freePort(t)
	postgresSecret := filepath.Join(root, "postgres-fixture-password")
	writeOwned(t, postgresSecret, []byte("disposable-postgresql-password"), 0, 0, 0o400)
	postgresData := filepath.Join(root, "postgres-data")
	mustMkdir(t, postgresData, 0o700)
	if err := dockerRun(docker, "create", "--pull", "never", "--name", postgresName, "--publish", "127.0.0.1:"+strconv.Itoa(int(postgresPort))+":5432/tcp", "--mount", "type=bind,src="+postgresData+",dst=/var/lib/postgresql/data", "--mount", "type=bind,src="+postgresSecret+",dst=/run/secrets/postgres-password,readonly", "--env", "POSTGRES_DB=authentik", "--env", "POSTGRES_USER=authentik", "--env", "POSTGRES_PASSWORD_FILE=/run/secrets/postgres-password", disposablePostgreSQLImage); err != nil {
		t.Fatal("create postgres fixture:", err)
	}
	if err := dockerRun(docker, "start", postgresName); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, 5*time.Minute, func() error {
		return dockerRun(docker, "exec", postgresName, "pg_isready", "--quiet", "--username", "authentik", "--dbname", "authentik")
	})
	stateRoot := filepath.Join(root, "state")
	dataRoot := filepath.Join(root, "data")
	secretRoot := filepath.Join(root, "secrets")
	for _, path := range []string{stateRoot, dataRoot, secretRoot} {
		mustMkdir(t, path, 0o700)
	}
	componentRoot := filepath.Join(dataRoot, "identity")
	mustMkdir(t, componentRoot, 0o700)
	for _, name := range []string{"data", "certs", "templates"} {
		path := filepath.Join(componentRoot, name)
		mustMkdir(t, path, 0o700)
		if err := os.Chown(path, 1000, 1000); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(componentRoot, "certs", "rollback-marker")
	writeOwned(t, marker, []byte("preserve-me"), 1000, 1000, 0o600)
	databaseSecret := []byte("disposable-postgresql-password")
	keySecret := []byte("disposable-authentik-secret-key-at-least-fifty-characters-1234567890")
	writeOwned(t, filepath.Join(secretRoot, "identity-postgresql-password"), databaseSecret, 0, 1000, 0o440)
	writeOwned(t, filepath.Join(secretRoot, "identity-secret-key"), keySecret, 0, 1000, 0o440)
	dockerBytes, err := os.ReadFile(docker)
	if err != nil {
		t.Fatal(err)
	}
	options := Options{DockerBinary: docker, DockerBinaryDigest: digest(dockerBytes), StateRoot: stateRoot, DataRoot: dataRoot, SecretRoot: secretRoot, ContainerPrefix: prefix, Timeout: 60 * time.Second}
	adapter, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adapter.Close() }()
	cleanupNames = append(cleanupNames, prefix+"-identity-server", prefix+"-identity-worker")
	defer func() {
		if t.Failed() {
			for _, name := range []string{prefix + "-identity-server", prefix + "-identity-worker"} {
				t.Log(name, dockerOutput(docker, "logs", "--tail", "120", name))
			}
		}
	}()
	ports := distinctPorts(t, 6, postgresPort)
	spec := Specification{Image: image, DatabaseHost: "127.0.0.1", DatabasePort: postgresPort, Database: "authentik", DatabaseRole: "authentik", HTTPPort: ports[0], HTTPSPort: ports[1], WorkerHTTPPort: ports[2], WorkerHTTPSPort: ports[3], ServerMetricsPort: ports[4], WorkerMetricsPort: ports[5], UID: 1000, GID: 1000}
	request := Request{OperationID: "fresh-install", ComponentID: "identity", Specification: spec, ConfigurationDigest: mustConfigDigest(spec), DatabaseSecretRevisionDigest: digest(databaseSecret), KeySecretRevisionDigest: digest(keySecret)}
	prepared, _, err := adapter.Preflight(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := adapter.CreateCandidate(context.Background(), "fresh-install", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := adapter.StartCandidate(context.Background(), "fresh-install", RoleServer); err != nil {
		t.Fatal(err)
	}
	waitAdapterRole(t, adapter, "fresh-install", RoleServer, DesiredCandidate, 7*time.Minute)
	if _, err := adapter.StartCandidate(context.Background(), "fresh-install", RoleWorker); err != nil {
		t.Fatal(err)
	}
	waitAdapterPair(t, adapter, "fresh-install", DesiredCandidate, 7*time.Minute)
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, err = Open(options)
	if err != nil {
		t.Fatal(err)
	}
	replacement := spec
	newPorts := distinctPorts(t, 2, postgresPort, spec.HTTPPort, spec.HTTPSPort, spec.WorkerHTTPPort, spec.WorkerHTTPSPort, spec.ServerMetricsPort, spec.WorkerMetricsPort)
	replacement.ServerMetricsPort = newPorts[0]
	replacement.WorkerMetricsPort = newPorts[1]
	replaceRequest := request
	replaceRequest.OperationID = "managed-replacement"
	replaceRequest.Specification = replacement
	replaceRequest.ConfigurationDigest = mustConfigDigest(replacement)
	prepared, _, err = adapter.Preflight(context.Background(), replaceRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := adapter.StopPrevious(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := adapter.PreservePrevious(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
		cleanupNames = append(cleanupNames, adapter.rollbackName(adapter.containerName("identity", role), "managed-replacement"))
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := adapter.CreateCandidate(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := adapter.StartCandidate(context.Background(), "managed-replacement", RoleServer); err != nil {
		t.Fatal(err)
	}
	waitAdapterRole(t, adapter, "managed-replacement", RoleServer, DesiredCandidate, 7*time.Minute)
	if _, err := adapter.StartCandidate(context.Background(), "managed-replacement", RoleWorker); err != nil {
		t.Fatal(err)
	}
	waitAdapterPair(t, adapter, "managed-replacement", DesiredCandidate, 7*time.Minute)
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := adapter.StopCandidate(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleWorker, RoleServer} {
		if _, err := adapter.RemoveCandidate(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
	}
	for _, role := range []Role{RoleServer, RoleWorker} {
		if _, err := adapter.RestorePrevious(context.Background(), "managed-replacement", role); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := adapter.RestartPrevious(context.Background(), "managed-replacement", RoleServer); err != nil {
		t.Fatal(RoleServer, err)
	}
	waitAdapterRole(t, adapter, "managed-replacement", RoleServer, DesiredPrevious, 7*time.Minute)
	if _, err := adapter.RestartPrevious(context.Background(), "managed-replacement", RoleWorker); err != nil {
		t.Fatal(RoleWorker, err)
	}
	waitAdapterPair(t, adapter, "managed-replacement", DesiredPrevious, 7*time.Minute)
	value, err := os.ReadFile(marker)
	if err != nil || string(value) != "preserve-me" {
		t.Fatalf("persistent marker: %q %v", value, err)
	}
	if err := adapter.RollbackStage(context.Background(), "managed-replacement"); err != nil {
		t.Fatal(err)
	}
}

func dockerRun(binary string, args ...string) error {
	command := exec.Command(binary, append([]string{"--host", dockerHost}, args...)...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "DOCKER_CONFIG=/tmp"}
	output, err := command.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(output)))
	}
	return nil
}
func dockerOutput(binary string, args ...string) string {
	command := exec.Command(binary, append([]string{"--host", dockerHost}, args...)...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "DOCKER_CONFIG=/tmp"}
	output, _ := command.CombinedOutput()
	return string(output)
}
func waitCommand(t *testing.T, limit time.Duration, probe func() error) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		if err := probe(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("disposable dependency did not become ready")
		}
		time.Sleep(time.Second)
	}
}
func waitAdapterRole(t *testing.T, adapter *Adapter, operation string, role Role, desired DesiredState, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		_, err := adapter.VerifyRole(context.Background(), operation, role, desired)
		if err == nil {
			return
		}
		if !errors.Is(err, ErrContainer) || time.Now().After(deadline) {
			t.Fatal(role, err)
		}
		time.Sleep(2 * time.Second)
	}
}
func waitAdapterPair(t *testing.T, adapter *Adapter, operation string, desired DesiredState, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		_, err := adapter.Verify(context.Background(), operation, desired)
		if err == nil {
			return
		}
		if !errors.Is(err, ErrContainer) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
	}
}
func freePort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}
func distinctPorts(t *testing.T, count int, used ...uint16) []uint16 {
	t.Helper()
	seen := map[uint16]bool{}
	for _, port := range used {
		seen[port] = true
	}
	result := make([]uint16, 0, count)
	for len(result) < count {
		port := freePort(t)
		if !seen[port] {
			seen[port] = true
			result = append(result, port)
		}
	}
	return result
}
func mustMkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
}
func writeOwned(t *testing.T, path string, value []byte, uid, gid int, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
