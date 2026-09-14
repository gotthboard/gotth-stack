//go:build linux

package postgresql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDisposablePostgreSQL17(t *testing.T) {
	binary := os.Getenv("GOTTH_POSTGRES_DOCKER_BIN")
	image := os.Getenv("GOTTH_POSTGRES_IMAGE")
	replacementImage := os.Getenv("GOTTH_POSTGRES_REPLACEMENT_IMAGE")
	if binary == "" || image == "" || replacementImage == "" {
		t.Skip("GOTTH_POSTGRES_DOCKER_BIN, GOTTH_POSTGRES_IMAGE, and GOTTH_POSTGRES_REPLACEMENT_IMAGE are not set")
	}
	if os.Geteuid() != 0 {
		t.Skip("disposable Docker proof must run as root")
	}
	const (
		component = "database"
		prefix    = "gotthpgproof"
		uid       = 999
		gid       = 999
	)
	primary := prefix + "-" + component
	operation := "disposable-operation"
	replacementOperation := "replacement-operation"
	rollback := primary + "-rollback-" + digest([]byte(operation))[len("sha256:"):len("sha256:")+12]
	replacementRollback := primary + "-rollback-" + digest([]byte(replacementOperation))[len("sha256:"):len("sha256:")+12]
	root, err := os.MkdirTemp("", "gotth-postgresql-proof.*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	removeContainer(t, binary, primary)
	removeContainer(t, binary, rollback)
	removeContainer(t, binary, replacementRollback)
	t.Cleanup(func() {
		removeContainer(t, binary, primary)
		removeContainer(t, binary, rollback)
		removeContainer(t, binary, replacementRollback)
	})
	stateRoot := filepath.Join(root, "state")
	dataRoot := filepath.Join(root, "data")
	secretRoot := filepath.Join(root, "secrets")
	for _, path := range []string{stateRoot, dataRoot, secretRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dataPath := filepath.Join(dataRoot, component)
	if err := os.Mkdir(dataPath, 0o700); err != nil || os.Chown(dataPath, uid, gid) != nil {
		t.Fatalf("prepare data directory: %v", err)
	}
	secret := []byte("disposable-postgresql-proof-password\n")
	secretPath := filepath.Join(secretRoot, component+"-password")
	if err := os.WriteFile(secretPath, secret, 0o440); err != nil || os.Chown(secretPath, 0, gid) != nil {
		t.Fatalf("prepare secret: %v", err)
	}
	specification := Specification{Image: image, Database: "gotth", Role: "gotth", Port: freeTCPPort(t), UID: uid, GID: gid}
	configuration, err := configurationDigest(specification)
	if err != nil {
		t.Fatal(err)
	}
	options := Options{
		DockerBinary: binary, DockerBinaryDigest: digestFile(t, binary), StateRoot: stateRoot,
		DataRoot: dataRoot, SecretRoot: secretRoot, ContainerPrefix: prefix, Timeout: 30 * time.Second,
	}
	adapter, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if adapter != nil && !adapter.closed {
			_ = adapter.Close()
		}
	})
	request := Request{
		OperationID: operation, ComponentID: component, Specification: specification,
		ConfigurationDigest: configuration, SecretRevisionDigest: digest(secret),
	}
	prepared, expected, err := adapter.Preflight(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.CreateCandidate(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.StartCandidate(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		actual, verifyErr := adapter.Verify(context.Background(), operation, DesiredCandidate)
		if verifyErr == nil {
			if actual != expected {
				t.Fatalf("verify summary = %#v, want %#v", actual, expected)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("candidate did not become ready: %v", verifyErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, err = Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Verify(context.Background(), operation, DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	replacementSpec := specification
	replacementSpec.Image = replacementImage
	replacementConfiguration, err := configurationDigest(replacementSpec)
	if err != nil {
		t.Fatal(err)
	}
	replacementRequest := Request{
		OperationID: replacementOperation, ComponentID: component, Specification: replacementSpec,
		ConfigurationDigest: replacementConfiguration, SecretRevisionDigest: digest(secret),
	}
	prepared, expected, err = adapter.Preflight(context.Background(), replacementRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !expected.HadPrevious || !expected.PreviousRunning {
		t.Fatalf("replacement did not bind the running previous container: %#v", expected)
	}
	if _, err := adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.StopPrevious(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PreservePrevious(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.CreateCandidate(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.StartCandidate(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		if _, verifyErr := adapter.Verify(context.Background(), replacementOperation, DesiredCandidate); verifyErr == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("replacement did not become ready: %v", verifyErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, err = Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Verify(context.Background(), replacementOperation, DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.StopCandidate(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RemoveCandidate(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RestorePrevious(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RestartPrevious(context.Background(), replacementOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Verify(context.Background(), replacementOperation, DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.StopCandidate(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RemoveCandidate(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Verify(context.Background(), operation, DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	if value, err := os.ReadFile(filepath.Join(dataPath, "PG_VERSION")); err != nil || string(value) != "17\n" {
		t.Fatalf("preserved PG_VERSION = %q, %v", value, err)
	}
}

func removeContainer(t *testing.T, binary, name string) {
	t.Helper()
	list := exec.Command(binary, "--host", dockerHost, "container", "ls", "--all", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	list.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "DOCKER_CONFIG=/tmp"}
	output, err := list.Output()
	if err != nil {
		t.Fatalf("list disposable container %q: %v", name, err)
	}
	if len(output) == 0 {
		return
	}
	if !bytes.Equal(output, []byte(name+"\n")) {
		t.Fatalf("unexpected disposable container listing %q", output)
	}
	remove := exec.Command(binary, "--host", dockerHost, "rm", "--force", name)
	remove.Env = list.Env
	if output, err := remove.CombinedOutput(); err != nil {
		t.Fatalf("remove disposable container %q: %v: %s", name, err, output)
	}
}

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}
