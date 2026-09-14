package authentik

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateArgumentsEnforceBoundary(t *testing.T) {
	spec := Specification{Image: "ghcr.io/goauthentik/server@sha256:" + strings.Repeat("a", 64), DatabaseHost: "127.0.0.1", DatabasePort: 55432, Database: "authentik", DatabaseRole: "authentik", HTTPPort: 19000, HTTPSPort: 19443, WorkerHTTPPort: 19001, WorkerHTTPSPort: 19444, ServerMetricsPort: 19300, WorkerMetricsPort: 19301, UID: 1000, GID: 1000}
	managed := managedSpec{OperationID: "operation-one", ComponentID: "identity", ConfigurationDigest: mustConfigDigest(spec), DatabaseSecretRevision: "sha256:" + strings.Repeat("b", 64), KeySecretRevision: "sha256:" + strings.Repeat("c", 64), Specification: spec, ImageID: "sha256:" + strings.Repeat("d", 64)}
	request := commandRequest{kind: commandCreate, name: "gotth-identity-worker", spec: managed, role: RoleWorker, dataPath: "/srv/identity/data", certsPath: "/srv/identity/certs", templatesPath: "/srv/identity/templates", databaseSecretPath: "/run/private/identity-postgresql-password", keySecretPath: "/run/private/identity-secret-key"}
	arguments, err := fixedArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	joined := " " + strings.Join(arguments, " ") + " "
	for _, required := range []string{" --pull never ", " --network host ", " --read-only ", " --cap-drop ALL ", " --security-opt no-new-privileges ", " AUTHENTIK_POSTGRESQL__PASSWORD=file:///run/secrets/postgresql-password ", " AUTHENTIK_SECRET_KEY=file:///run/secrets/secret-key ", " AUTHENTIK_LISTEN__HTTP=127.0.0.1:19001 ", " AUTHENTIK_LISTEN__METRICS=127.0.0.1:19301 ", " ghcr.io/goauthentik/server@sha256:", " worker "} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"dst=/var/run/docker.sock", "--privileged", "--cap-add", "--publish", "docker pull", "AUTHENTIK_SECRET_KEY=authentik"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("forbidden %q", forbidden)
		}
	}
}

func TestSpecificationRejectsUnsafeTopology(t *testing.T) {
	base := Specification{Image: "ghcr.io/goauthentik/server@sha256:" + strings.Repeat("a", 64), DatabaseHost: "127.0.0.1", DatabasePort: 55432, Database: "authentik", DatabaseRole: "authentik", HTTPPort: 19000, HTTPSPort: 19443, WorkerHTTPPort: 19001, WorkerHTTPSPort: 19444, ServerMetricsPort: 19300, WorkerMetricsPort: 19301, UID: 1000, GID: 1000}
	cases := []func(*Specification){func(v *Specification) { v.DatabaseHost = "postgres.example" }, func(v *Specification) { v.HTTPPort = v.DatabasePort }, func(v *Specification) { v.UID = 0 }, func(v *Specification) { v.Image = "ghcr.io/goauthentik/server:2026.5.2" }}
	for i, mutate := range cases {
		value := base
		mutate(&value)
		if err := validateSpecification(value); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestNormalizeRejectsDockerSocketAndExtraAuthentikControl(t *testing.T) {
	f := newFixture(t)
	managed := managedSpec{OperationID: "operation-one", ComponentID: "identity", ConfigurationDigest: mustConfigDigest(f.spec), DatabaseSecretRevision: digest(f.db), KeySecretRevision: digest(f.key), Specification: f.spec, ImageID: imageDigest(f.spec.Image)}
	request := commandRequest{kind: commandCreate, spec: managed, role: RoleWorker, dataPath: f.dataDir("data"), certsPath: f.dataDir("certs"), templatesPath: f.dataDir("templates"), databaseSecretPath: filepath.Join(f.secrets, "identity-postgresql-password"), keySecretPath: filepath.Join(f.secrets, "identity-secret-key")}
	record := fakeInspect("gotth-identity-worker", fakeContainer{request: request, id: strings.Repeat("a", 64)})
	record.Mounts = append(record.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{"bind", "/var/run/docker.sock", "/var/run/docker.sock", true})
	if _, err := f.adapter.normalizeContainer("gotth-identity-worker", record); err != ErrContainer {
		t.Fatalf("socket mount: %v", err)
	}
	record = fakeInspect("gotth-identity-worker", fakeContainer{request: request, id: strings.Repeat("a", 64)})
	record.Config.Env = append(record.Config.Env, "AUTHENTIK_BOOTSTRAP_PASSWORD=bad")
	if _, err := f.adapter.normalizeContainer("gotth-identity-worker", record); err != ErrContainer {
		t.Fatalf("extra env: %v", err)
	}
}

func TestFixedArgumentsCoverEveryDockerOperation(t *testing.T) {
	f := newFixture(t)
	managed := managedSpec{OperationID: "operation-one", ComponentID: "identity", ConfigurationDigest: mustConfigDigest(f.spec), DatabaseSecretRevision: digest(f.db), KeySecretRevision: digest(f.key), Specification: f.spec, ImageID: imageDigest(f.spec.Image)}
	create := commandRequest{kind: commandCreate, name: "gotth-identity-server", spec: managed, role: RoleServer, dataPath: f.dataDir("data"), certsPath: f.dataDir("certs"), templatesPath: f.dataDir("templates"), databaseSecretPath: filepath.Join(f.secrets, "identity-postgresql-password"), keySecretPath: filepath.Join(f.secrets, "identity-secret-key")}
	cases := []commandRequest{
		{kind: commandVersion},
		{kind: commandImageInspect, spec: managed},
		{kind: commandContainerList, name: create.name},
		{kind: commandContainerInspect, name: create.name},
		{kind: commandStop, name: create.name, timeoutSec: 30},
		{kind: commandRename, name: create.name, destination: create.name + "-old"},
		create,
		{kind: commandStart, name: create.name},
		{kind: commandRemove, name: create.name},
		{kind: commandHealth, name: create.name, role: RoleServer},
	}
	for _, request := range cases {
		arguments, err := fixedArguments(request)
		if err != nil {
			t.Fatalf("kind %d: %v", request.kind, err)
		}
		if len(arguments) < 3 || arguments[0] != "--host" || arguments[1] != dockerHost {
			t.Fatalf("kind %d: %q", request.kind, arguments)
		}
	}
	invalid := []commandRequest{
		{kind: commandImageInspect},
		{kind: commandContainerList},
		{kind: commandContainerInspect},
		{kind: commandStop, name: create.name},
		{kind: commandRename, name: create.name},
		{kind: commandStart},
		{kind: commandRemove},
		{kind: commandHealth, name: create.name, role: "invalid"},
		{kind: commandKind(255)},
	}
	for _, request := range invalid {
		if _, err := fixedArguments(request); err != ErrInvalidInput {
			t.Fatalf("invalid kind %d: %v", request.kind, err)
		}
	}
}

func TestExecRunnerAndOutputLimit(t *testing.T) {
	if err := (execRunner{}).Run(context.Background(), "/usr/bin/true", commandRequest{kind: commandVersion}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	buffer := &limitedBuffer{limit: 3}
	if count, err := buffer.Write([]byte("abcd")); count != 3 || err != ErrLimit || string(buffer.value) != "abc" {
		t.Fatalf("partial limit write = %d %q %v", count, buffer.value, err)
	}
	if count, err := buffer.Write([]byte("x")); count != 0 || err != ErrLimit {
		t.Fatalf("full limit write = %d %v", count, err)
	}
}

func (f *fixture) dataDir(name string) string { return filepath.Join(f.data, "identity", name) }
