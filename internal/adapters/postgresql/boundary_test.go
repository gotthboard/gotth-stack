//go:build linux

package postgresql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFixedDockerArgumentsAndOutputBounds(t *testing.T) {
	specification := Specification{Image: testImageA, Database: "gotth", Role: "gotth", Port: 15432, UID: 999, GID: 999}
	configuration, err := configurationDigest(specification)
	if err != nil {
		t.Fatal(err)
	}
	spec := managedSpec{
		OperationID: "operation-one", ComponentID: "postgres-main", ConfigurationDigest: configuration,
		SecretRevision: digest([]byte("secret")), Specification: specification,
	}
	create, err := fixedArguments(commandRequest{
		kind: commandCreate, name: "gotth-postgres-main", spec: spec,
		dataPath: "/srv/postgres/postgres-main", secretPath: "/run/gotth/secrets/postgres-main-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--host", dockerHost, "create", "--pull", "never", "--name", "gotth-postgres-main",
		"--label", labelAdapter + "=" + adapterLabelValue,
		"--label", labelComponent + "=postgres-main",
		"--label", labelConfiguration + "=" + configuration,
		"--label", labelSecretRevision + "=" + spec.SecretRevision,
		"--label", labelOperation + "=operation-one",
		"--user", "999:999", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--publish", "127.0.0.1:15432:5432/tcp",
		"--mount", "type=bind,src=/srv/postgres/postgres-main,dst=" + dataTarget,
		"--mount", "type=bind,src=/run/gotth/secrets/postgres-main-password,dst=" + secretTarget + ",readonly",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=67108864",
		"--tmpfs", "/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216",
		"--env", "POSTGRES_DB=gotth", "--env", "POSTGRES_USER=gotth", "--env", "POSTGRES_PASSWORD_FILE=" + secretTarget,
		testImageA,
	}
	if !reflect.DeepEqual(create, want) {
		t.Fatalf("create arguments = %#v\nwant %#v", create, want)
	}
	list, err := fixedArguments(commandRequest{kind: commandContainerList, name: "gotth-postgres-main"})
	if err != nil {
		t.Fatal(err)
	}
	wantList := []string{"--host", dockerHost, "container", "ls", "--all", "--filter", "name=^/gotth-postgres-main$", "--format", "{{.Names}}"}
	if !reflect.DeepEqual(list, wantList) {
		t.Fatalf("list = %#v", list)
	}
	for _, request := range []commandRequest{
		{}, {kind: commandImageInspect}, {kind: commandContainerList}, {kind: commandContainerInspect},
		{kind: commandStop, name: "container", timeoutSec: 0}, {kind: commandStop, name: "container", timeoutSec: 61},
		{kind: commandRename, name: "container"}, {kind: commandCreate, name: "container"},
		{kind: commandStart}, {kind: commandRemove}, {kind: commandReady}, {kind: commandIdentity},
	} {
		if _, err := fixedArguments(request); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("request %#v = %v", request, err)
		}
	}
	joined := strings.Join(create, "\x00")
	for _, forbidden := range []string{"/bin/sh", "/bin/bash", " pull\x00", "volume", "--privileged", "--network=host"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("forbidden argument %q in %q", forbidden, joined)
		}
	}
	buffer := &limitedBuffer{limit: 2}
	if count, err := buffer.Write([]byte("abc")); count != 2 || !errors.Is(err, ErrLimit) {
		t.Fatalf("bounded write = %d, %v", count, err)
	}
	if count, err := buffer.Write([]byte("x")); count != 0 || !errors.Is(err, ErrLimit) {
		t.Fatalf("full write = %d, %v", count, err)
	}
	var output bytes.Buffer
	request := commandRequest{kind: commandVersion, environment: []string{"PATH=/usr/bin:/bin"}, workingDir: "/tmp"}
	if err := (execRunner{}).Run(context.Background(), "/bin/true", request, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestOpenBoundariesLockAndBinaryDigest(t *testing.T) {
	fixture := newFixture(t)
	if _, err := Open(fixture.options); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open = %v", err)
	}
	for _, timeout := range []time.Duration{-time.Second, time.Millisecond, time.Minute + time.Second} {
		options := fixture.options
		options.Timeout = timeout
		if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("timeout %v = %v", timeout, err)
		}
	}
	options := fixture.options
	options.DataRoot = options.StateRoot
	if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("overlap = %v", err)
	}
	options = fixture.options
	options.DataRoot += ",readonly"
	if _, err := Open(options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mount delimiter = %v", err)
	}
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	options = fixture.options
	options.DockerBinaryDigest = digest([]byte("wrong binary"))
	if _, err := Open(options); !errors.Is(err, ErrExecutable) {
		t.Fatalf("binary digest = %v", err)
	}
}

func TestUnsafeStorageAndSecretObjectsAreRejected(t *testing.T) {
	t.Run("state mode", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fixture.options.StateRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("state mode = %v", err)
		}
	})
	t.Run("data parent mode", func(t *testing.T) {
		fixture := newFixture(t)
		if err := fixture.adapter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fixture.options.DataRoot, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(fixture.options); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("data parent mode = %v", err)
		}
	})
	t.Run("secret symlink", func(t *testing.T) {
		fixture := newFixture(t)
		secret := filepath.Join(fixture.options.SecretRoot, "postgres-main-password")
		real := secret + ".real"
		if err := os.Rename(secret, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, secret); err != nil {
			t.Fatal(err)
		}
		if _, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "secret-link", fixture.spec)); !errors.Is(err, ErrSecret) {
			t.Fatalf("secret symlink = %v", err)
		}
	})
	t.Run("wrong major", func(t *testing.T) {
		fixture := newFixture(t)
		if err := os.WriteFile(filepath.Join(fixture.options.DataRoot, "postgres-main", "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := fixture.adapter.Preflight(context.Background(), fixture.request(t, "wrong-major", fixture.spec)); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("wrong major = %v", err)
		}
	})
}

func TestTamperedAndIncompleteTransactionsFailClosed(t *testing.T) {
	fixture := newFixture(t)
	fixture.preflightAndStage(t, "operation-five", fixture.spec)
	path := filepath.Join(fixture.options.StateRoot, transactionsName, transactionName("operation-five"))
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Observe(context.Background(), "operation-five"); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("tampered transaction = %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture.options.StateRoot, transactionsName, stageName("operation-six")), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := fixture.adapter.Observe(context.Background(), "operation-six")
	if err != nil || observation.StageState != StateIncomplete {
		t.Fatalf("incomplete observation = %#v, %v", observation, err)
	}
	if _, state, err := fixture.adapter.ReconcileStage("operation-six"); state != StateIncomplete || !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("incomplete reconcile = %q, %v", state, err)
	}
}

func TestInputAndContainerNormalizationRejectDrift(t *testing.T) {
	fixture := newFixture(t)
	valid := fixture.request(t, "operation-seven", fixture.spec)
	cases := []Request{
		{OperationID: "../escape"},
		{OperationID: valid.OperationID, ComponentID: "UPPER", Specification: valid.Specification, ConfigurationDigest: valid.ConfigurationDigest, SecretRevisionDigest: valid.SecretRevisionDigest},
		{OperationID: valid.OperationID, ComponentID: valid.ComponentID, Specification: valid.Specification, ConfigurationDigest: digest([]byte("wrong")), SecretRevisionDigest: valid.SecretRevisionDigest},
	}
	for _, request := range cases {
		if _, _, err := fixture.adapter.Preflight(context.Background(), request); err == nil {
			t.Errorf("request unexpectedly accepted: %#v", request)
		}
	}
	managed := managedSpec{
		OperationID: valid.OperationID, ComponentID: valid.ComponentID, ConfigurationDigest: valid.ConfigurationDigest,
		SecretRevision: valid.SecretRevisionDigest, Specification: valid.Specification, ImageID: imageDigest(valid.Specification.Image),
		ContainerName: "gotth-postgres-main", DataName: valid.ComponentID, SecretName: valid.ComponentID + "-password",
	}
	record := fakeContainerInspect(managed.ContainerName, &containerRecord{spec: managed, id: fakeContainerID("record")}, fixture.options.DataRoot)
	if _, err := fixture.adapter.normalizeContainer(managed.ContainerName, record); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*containerInspect){
		"privileged":            func(value *containerInspect) { value.HostConfig.Privileged = true },
		"writable root":         func(value *containerInspect) { value.HostConfig.ReadonlyRootfs = false },
		"capability":            func(value *containerInspect) { value.HostConfig.CapAdd = []string{"NET_ADMIN"} },
		"security":              func(value *containerInspect) { value.HostConfig.SecurityOpt = nil },
		"port exposure":         func(value *containerInspect) { value.HostConfig.PortBindings["5432/tcp"][0].HostIP = "0.0.0.0" },
		"tmpfs":                 func(value *containerInspect) { delete(value.HostConfig.Tmpfs, "/tmp") },
		"mount":                 func(value *containerInspect) { value.Mounts[0].Source = "/wrong" },
		"label":                 func(value *containerInspect) { value.Config.Labels[labelAdapter] = "other" },
		"foreign managed label": func(value *containerInspect) { value.Config.Labels["com.gotthstack.hidden"] = "bad" },
		"environment":           func(value *containerInspect) { value.Config.Env = append(value.Config.Env, "POSTGRES_DB=duplicate") },
		"password environment":  func(value *containerInspect) { value.Config.Env = append(value.Config.Env, "POSTGRES_PASSWORD=bad") },
		"entrypoint":            func(value *containerInspect) { value.Config.Entrypoint = []string{"sh"} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := fakeContainerInspect(managed.ContainerName, &containerRecord{spec: managed, id: fakeContainerID("record")}, fixture.options.DataRoot)
			mutate(&changed)
			if _, err := fixture.adapter.normalizeContainer(managed.ContainerName, changed); !errors.Is(err, ErrContainer) {
				t.Fatalf("drift = %v", err)
			}
		})
	}
}

func TestFinalTransactionWithStageResidueRequiresRecovery(t *testing.T) {
	fixture := newFixture(t)
	fixture.preflightAndStage(t, "operation-ten", fixture.spec)
	stage := filepath.Join(fixture.options.StateRoot, transactionsName, stageName("operation-ten"))
	if err := os.WriteFile(stage, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Observe(context.Background(), "operation-ten"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("observation ignored stage residue: %v", err)
	}
	if _, _, err := fixture.adapter.ReconcileStage("operation-ten"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("reconcile ignored stage residue: %v", err)
	}
}

func TestDecodersAndSmallValidatorsRejectAmbiguity(t *testing.T) {
	var target map[string]any
	for _, value := range [][]byte{nil, []byte("no"), []byte("{} {}"), bytes.Repeat([]byte{'x'}, MaxOutputBytes+1)} {
		if err := decodeOne(value, &target); err == nil {
			t.Errorf("decode accepted %d bytes", len(value))
		}
	}
	if err := decodeOne([]byte("{\"a\":1}\n"), &target); err != nil {
		t.Fatal(err)
	}
	if value, ok := uniqueEnvironment([]string{"A=1", "A=2"}, "A"); ok || value != "" {
		t.Fatalf("duplicate environment = %q, %t", value, ok)
	}
	for _, value := range []string{"", "0", "1", "1:2:3", "a:b", "0:1", "1:0"} {
		if _, _, ok := parseUser(value); ok {
			t.Errorf("user %q accepted", value)
		}
	}
	if _, ok := exactPort(map[string][]portMap{"5432/tcp": {{HostIP: "0.0.0.0", HostPort: "5432"}}}); ok {
		t.Fatal("public port accepted")
	}
	if _, ok := exactPort(map[string][]portMap{"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "0"}}}); ok {
		t.Fatal("zero port accepted")
	}
	if imageDigest("postgres:17") != "" {
		t.Fatal("tag-only image produced a digest")
	}
	for _, specification := range []Specification{
		{}, {Image: "postgres:17", Database: "gotth", Role: "gotth", Port: 1, UID: 1, GID: 1},
		{Image: testImageA, Database: "UPPER", Role: "gotth", Port: 1, UID: 1, GID: 1},
		{Image: testImageA, Database: "gotth", Role: "gotth", Port: 1, UID: 0, GID: 1},
	} {
		if err := validateSpecification(specification); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("specification %#v = %v", specification, err)
		}
	}
}
