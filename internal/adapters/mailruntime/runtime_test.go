package mailruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeRuntimeContainer struct {
	spec       managedSpec
	definition roleDefinition
	running    bool
	id         string
}

type fakeRuntimeDocker struct {
	containers     map[string]fakeRuntimeContainer
	fail           commandKind
	network        bool
	healthFailures int
	healthChecks   int
}

func (runner *fakeRuntimeDocker) Run(_ context.Context, _ string, request commandRequest, stdout, _ io.Writer) error {
	if runner.fail == request.kind {
		return errors.New("injected")
	}
	switch request.kind {
	case commandVersion:
		return json.NewEncoder(stdout).Encode(map[string]any{"Version": "28.0.1", "ApiVersion": "1.48", "Os": "linux", "Arch": "amd64"})
	case commandImageInspect:
		record := imageInspect{ID: imageDigest(request.spec.Candidate.Image), RepoDigests: []string{request.spec.Candidate.Image}}
		record.Config.User = request.definition.user
		record.Config.Entrypoint = []string{"/usr/local/bin/gotth-mail-entrypoint"}
		record.Config.Labels = map[string]string{imageLabelRole: string(request.spec.Role), imageLabelSource: request.spec.Candidate.SourceCommit, imageLabelVersion: request.spec.Candidate.ProductVersion}
		return json.NewEncoder(stdout).Encode([]imageInspect{record})
	case commandNetworkList:
		if runner.network {
			_, _ = io.WriteString(stdout, request.name+"\n")
		}
		return nil
	case commandNetworkInspect:
		if !runner.network {
			return errors.New("absent")
		}
		var record networkInspect
		record.Name = request.name
		record.ID = strings.Repeat("5", 64)
		record.Scope = "local"
		record.Driver = "bridge"
		record.Labels = map[string]string{"com.gotthstack.network": "mailruntime.v1"}
		record.IPAM.Driver = "default"
		record.IPAM.Config = append(record.IPAM.Config, struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		}{Subnet: "172.30.0.0/24", Gateway: "172.30.0.1"})
		return json.NewEncoder(stdout).Encode([]networkInspect{record})
	case commandNetworkCreate:
		if runner.network {
			return errors.New("exists")
		}
		runner.network = true
		return nil
	case commandContainerList:
		if _, ok := runner.containers[request.name]; ok {
			_, _ = io.WriteString(stdout, request.name+"\n")
		}
		return nil
	case commandContainerInspect:
		container, ok := runner.containers[request.name]
		if !ok {
			return errors.New("absent")
		}
		return json.NewEncoder(stdout).Encode([]containerInspect{fakeRuntimeInspect(request.name, container)})
	case commandStop:
		container, ok := runner.containers[request.name]
		if !ok {
			return errors.New("absent")
		}
		container.running = false
		runner.containers[request.name] = container
		return nil
	case commandRename:
		container, ok := runner.containers[request.name]
		if !ok {
			return errors.New("absent")
		}
		if _, exists := runner.containers[request.destination]; exists {
			return errors.New("exists")
		}
		delete(runner.containers, request.name)
		runner.containers[request.destination] = container
		return nil
	case commandCreate:
		if _, exists := runner.containers[request.name]; exists {
			return errors.New("exists")
		}
		runner.containers[request.name] = fakeRuntimeContainer{spec: request.spec, definition: request.definition, id: strings.Repeat("1", 64)}
		return nil
	case commandStart:
		container, ok := runner.containers[request.name]
		if !ok {
			return errors.New("absent")
		}
		container.running = true
		runner.containers[request.name] = container
		return nil
	case commandRemove:
		container, ok := runner.containers[request.name]
		if !ok || container.running {
			return errors.New("unsafe")
		}
		delete(runner.containers, request.name)
		return nil
	case commandHealth:
		runner.healthChecks++
		if runner.healthFailures > 0 {
			runner.healthFailures--
			return errors.New("starting")
		}
		container, ok := runner.containers[request.name]
		if !ok || !container.running {
			return errors.New("unhealthy")
		}
		return nil
	default:
		return ErrInvalidInput
	}
}

func fakeRuntimeInspect(name string, container fakeRuntimeContainer) containerInspect {
	spec := container.spec
	definition := container.definition
	var record containerInspect
	record.ID = container.id
	record.Name = "/" + name
	record.Image = spec.ImageID
	record.Config.Image = spec.Candidate.Image
	record.Config.User = definition.user
	record.Config.Env = []string{"GOTTH_MAIL_CONFIG_DIR=" + configTarget}
	record.Config.Entrypoint = []string{"/usr/local/bin/gotth-mail-entrypoint"}
	record.Config.Labels = map[string]string{
		labelAdapter: definition.adapterID, labelComponent: spec.Candidate.ComponentID,
		labelConfiguration: spec.Candidate.ConfigurationDigest, labelSecretSet: spec.Candidate.SecretRevisionDigest,
		labelConfigSize: strconv.FormatInt(spec.Candidate.ConfigurationSize, 10),
		labelOperation:  spec.Candidate.OperationID, labelVersion: spec.Candidate.ProductVersion, labelSourceCommit: spec.Candidate.SourceCommit,
	}
	record.HostConfig.ReadonlyRootfs = true
	record.HostConfig.NetworkMode = spec.NetworkName
	record.HostConfig.CapDrop = []string{"ALL"}
	record.HostConfig.CapAdd = append([]string(nil), definition.capAdd...)
	record.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	record.HostConfig.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=67108864", "/run": "rw,noexec,nosuid,nodev,size=16777216"}
	record.HostConfig.PortBindings = make(map[string][]portBinding)
	for _, port := range definition.ports {
		record.HostConfig.PortBindings[strconvInt(port.containerPort)+"/tcp"] = []portBinding{{HostIP: port.hostIP, HostPort: strconvInt(port.hostPort)}}
	}
	for index, mount := range definition.mounts {
		record.Mounts = append(record.Mounts, struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		}{Type: "bind", Source: spec.MountSources[index], Destination: mount.destination, RW: !mount.readOnly})
	}
	record.NetworkSettings.Networks = map[string]struct {
		Aliases []string `json:"Aliases"`
	}{spec.NetworkName: {Aliases: []string{name, definition.alias}}}
	record.State.Running = container.running
	return record
}

func strconvInt(value uint16) string {
	return strconv.Itoa(int(value))
}

type runtimeFixture struct {
	t             *testing.T
	adapter       *Postfix
	runner        *fakeRuntimeDocker
	configuration []FileArtifact
	configDigest  string
	configSize    int64
	revision      string
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	root := t.TempDir()
	contents := []byte("compatibility_level = 3.6\n")
	archive, members := configurationArchive(t, []archiveInput{{name: "main.cf", contents: string(contents)}}, nil)
	sum := sha256.Sum256(archive)
	configDigest := "sha256:" + hex.EncodeToString(sum[:])
	state := filepath.Join(root, "state")
	configStore := filepath.Join(root, "config-store")
	config := filepath.Join(configStore, strings.TrimPrefix(configDigest, "sha256:"))
	queue := filepath.Join(root, "queue")
	for _, path := range []string{state, configStore, config, queue} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configFile := filepath.Join(config, "main.cf")
	if err := os.WriteFile(configFile, contents, configurationMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configFile, configurationMode); err != nil {
		t.Fatal(err)
	}
	binary := "/usr/bin/true"
	value, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	secretValue := []byte("value\n")
	secretSum := sha256.Sum256(secretValue)
	secretMembers := []revisionMember{
		{Destination: postfixHelperTarget, Digest: "sha256:" + hex.EncodeToString(secretSum[:]), Size: int64(len(secretValue))},
		{Destination: postfixReleaseTarget, Digest: "sha256:" + hex.EncodeToString(secretSum[:]), Size: int64(len(secretValue))},
	}
	encodedSecrets, err := json.Marshal(secretMembers)
	if err != nil {
		t.Fatal(err)
	}
	secretRevision := digest(encodedSecrets)
	secretRoot := filepath.Join(root, "secret-store", strings.TrimPrefix(secretRevision, "sha256:"))
	if err := os.MkdirAll(secretRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(secretRoot, "helper-token")
	releasePath := filepath.Join(secretRoot, "release-token")
	for _, path := range []string{helperPath, releasePath} {
		if err := os.WriteFile(path, secretValue, 0o400); err != nil {
			t.Fatal(err)
		}
	}
	adapter, err := OpenPostfix(PostfixOptions{Runtime: RuntimeOptions{DockerBinary: binary, DockerBinaryDigest: digest(value), StateRoot: state, ConfigurationRoot: config, ContainerPrefix: "gotth", Timeout: 5 * time.Second}, QueueRoot: queue, HelperSecretFile: helperPath, ReleaseSecretFile: releasePath})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRuntimeDocker{containers: make(map[string]fakeRuntimeContainer), network: true}
	adapter.adapter.runner = runner
	revision, err := adapter.revisionDigest()
	if err != nil {
		t.Fatal(err)
	}
	return &runtimeFixture{t: t, adapter: adapter, runner: runner, configuration: members, configDigest: configDigest, configSize: int64(len(archive)), revision: revision}
}

func (fixture *runtimeFixture) close() {
	fixture.t.Helper()
	if err := fixture.adapter.Close(); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *runtimeFixture) candidate(operation, imageCharacter, sourceCharacter string) Candidate {
	return Candidate{
		OperationID: operation, ComponentID: "mail", ProductVersion: "1.0.0-alpha.1",
		SourceCommit: strings.Repeat(sourceCharacter, 40), Image: roleImageRepositories[RolePostfix] + "@sha256:" + strings.Repeat(imageCharacter, 64),
		ConfigurationDigest: fixture.configDigest, ConfigurationSize: fixture.configSize, ConfigurationMembers: fixture.configuration, SecretRevisionDigest: fixture.revision,
	}
}

func (fixture *runtimeFixture) preflight(candidate Candidate) (*Prepared, Summary) {
	fixture.t.Helper()
	prepared, summary, err := fixture.adapter.Preflight(context.Background(), PostfixRequest{Candidate: candidate})
	if err != nil {
		fixture.t.Fatal(err)
	}
	return prepared, summary
}

func TestFreshInstallAndRollbackStateMachine(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("fresh-install", "a", "b")
	prepared, summary := fixture.preflight(candidate)
	if summary.HadPrevious {
		t.Fatal("fresh install reported previous runtime")
	}
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.RenamePrevious, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate, fixture.adapter.VerifyCandidate} {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	observation, err := fixture.adapter.Observe(context.Background(), candidate.OperationID)
	if err != nil || observation.PrimaryState != StateCandidate || observation.PrimaryPower != PowerRunning || observation.RollbackState != StateAbsent {
		t.Fatalf("observation=%#v error=%v", observation, err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopCandidate, fixture.adapter.RemoveCandidate, fixture.adapter.RestorePreviousName, fixture.adapter.StartPrevious, fixture.adapter.VerifyPrevious} {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyCandidateRetriesBoundedColdStart(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("cold-start", "a", "b")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.RenamePrevious, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate} {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	fixture.runner.healthFailures = 2
	if _, err := fixture.adapter.VerifyCandidate(context.Background(), candidate.OperationID); err != nil {
		t.Fatal(err)
	}
	if fixture.runner.healthChecks != 3 {
		t.Fatalf("health checks=%d, want 3", fixture.runner.healthChecks)
	}
}

func TestReplacementCommitAndRollback(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			fixture := newRuntimeFixture(t)
			defer fixture.close()
			previous := fixture.candidate("initial", "a", "b")
			previousSpec := managedSpec{Candidate: previous, Role: RolePostfix, ImageID: imageDigest(previous.Image), ContainerName: "gotth-mail-postfix", NetworkName: "gotth-private", MountSources: fixture.adapter.candidateMountSources()}
			fixture.runner.containers["gotth-mail-postfix"] = fakeRuntimeContainer{spec: previousSpec, definition: fixture.adapter.definition, running: true, id: strings.Repeat("2", 64)}
			candidate := fixture.candidate("replace-runtime", "c", "d")
			prepared, summary := fixture.preflight(candidate)
			if !summary.HadPrevious || !summary.PreviousRunning {
				t.Fatalf("summary=%#v", summary)
			}
			if _, err := prepared.Stage(); err != nil {
				t.Fatal(err)
			}
			for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.RenamePrevious, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate, fixture.adapter.VerifyCandidate} {
				if _, err := action(context.Background(), candidate.OperationID); err != nil {
					t.Fatal(err)
				}
			}
			if commit {
				if _, err := fixture.adapter.CommitCandidate(context.Background(), candidate.OperationID); err != nil {
					t.Fatal(err)
				}
				if len(fixture.runner.containers) != 1 || fixture.runner.containers["gotth-mail-postfix"].spec.Candidate.Image != candidate.Image {
					t.Fatalf("containers=%#v", fixture.runner.containers)
				}
				return
			}
			for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopCandidate, fixture.adapter.RemoveCandidate, fixture.adapter.RestorePreviousName, fixture.adapter.StartPrevious, fixture.adapter.VerifyPrevious} {
				if _, err := action(context.Background(), candidate.OperationID); err != nil {
					t.Fatal(err)
				}
			}
			if len(fixture.runner.containers) != 1 || fixture.runner.containers["gotth-mail-postfix"].spec.Candidate.Image != previous.Image || !fixture.runner.containers["gotth-mail-postfix"].running {
				t.Fatalf("containers=%#v", fixture.runner.containers)
			}
		})
	}
}

func TestMutationFailureRequiresRecovery(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("failed-create", "a", "b")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	fixture.runner.fail = commandCreate
	if _, err := fixture.adapter.CreateCandidate(context.Background(), candidate.OperationID); err != ErrRecoveryRequired {
		t.Fatalf("error=%v", err)
	}
}

func TestEnsureNetworkIsBoundedAndIdempotent(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	fixture.runner.network = false
	control := &ControlPlane{adapter: fixture.adapter.adapter}
	first, err := control.EnsureNetwork(context.Background())
	if err != nil || first.Name != "gotth-private" || !digestPattern.MatchString(first.Digest) {
		t.Fatalf("summary=%#v error=%v", first, err)
	}
	second, err := control.EnsureNetwork(context.Background())
	if err != nil || second != first {
		t.Fatalf("summary=%#v error=%v", second, err)
	}
}

func TestPreflightRequiresAdmittedNetwork(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	fixture.runner.network = false
	candidate := fixture.candidate("missing-network", "a", "b")
	if _, _, err := fixture.adapter.Preflight(context.Background(), PostfixRequest{Candidate: candidate}); !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestPreflightRejectsConfigurationBoundaryChanges(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*runtimeFixture, *Candidate)
	}{
		{name: "archive digest", mutate: func(_ *runtimeFixture, candidate *Candidate) { candidate.ConfigurationDigest = testDigest }},
		{name: "archive size", mutate: func(_ *runtimeFixture, candidate *Candidate) { candidate.ConfigurationSize-- }},
		{name: "member digest", mutate: func(_ *runtimeFixture, candidate *Candidate) { candidate.ConfigurationMembers[0].Digest = testDigest }},
		{name: "member size", mutate: func(_ *runtimeFixture, candidate *Candidate) { candidate.ConfigurationMembers[0].Size++ }},
		{name: "extra file", mutate: func(fixture *runtimeFixture, _ *Candidate) {
			if err := os.WriteFile(filepath.Join(fixture.adapter.configPath, "extra"), []byte("x"), configurationMode); err != nil {
				fixture.t.Fatal(err)
			}
		}},
		{name: "unsafe mode", mutate: func(fixture *runtimeFixture, _ *Candidate) {
			if err := os.Chmod(filepath.Join(fixture.adapter.configPath, "main.cf"), 0o640); err != nil {
				fixture.t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRuntimeFixture(t)
			defer fixture.close()
			candidate := fixture.candidate("config-boundary", "a", "b")
			tc.mutate(fixture, &candidate)
			if _, _, err := fixture.adapter.Preflight(context.Background(), PostfixRequest{Candidate: candidate}); !errors.Is(err, ErrConfiguration) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestMutationRejectsMountReplacement(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("mount-swap", "a", "b")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	queue := fixture.adapter.definition.mounts[1].source
	replacement := queue + "-replacement"
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(queue, queue+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, queue); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.CreateCandidate(context.Background(), candidate.OperationID); !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestStageIdempotenceConflictAndRollback(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("stage-only", "a", "b")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	conflict := *prepared
	conflict.metadata.Candidate.Candidate.SourceCommit = strings.Repeat("c", 40)
	if _, err := conflict.Stage(); !errors.Is(err, ErrInvalidInput) && !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error=%v", err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), candidate.OperationID); err != nil {
		t.Fatal(err)
	}
	observation, err := fixture.adapter.Observe(context.Background(), candidate.OperationID)
	if err != nil || observation.StageState != StateAbsent {
		t.Fatalf("observation=%#v error=%v", observation, err)
	}
}

func TestActionsAreIdempotentAtExactSuccessorState(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	candidate := fixture.candidate("idempotent-install", "a", "b")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	actions := []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.RenamePrevious, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate, fixture.adapter.VerifyCandidate, fixture.adapter.CommitCandidate}
	for _, action := range actions {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatalf("idempotent retry: %v", err)
		}
	}
}

func TestReplacementSupportsImmutableConfigurationRevision(t *testing.T) {
	fixture := newRuntimeFixture(t)
	defer fixture.close()
	previous := fixture.candidate("initial-config", "a", "b")
	previousSpec := managedSpec{Candidate: previous, Role: RolePostfix, ImageID: imageDigest(previous.Image), ContainerName: "gotth-mail-postfix", NetworkName: "gotth-private", MountSources: fixture.adapter.candidateMountSources()}
	fixture.runner.containers["gotth-mail-postfix"] = fakeRuntimeContainer{spec: previousSpec, definition: fixture.adapter.definition, running: true, id: strings.Repeat("3", 64)}
	contents := "compatibility_level = 3.8\n"
	archive, members := configurationArchive(t, []archiveInput{{name: "main.cf", contents: contents}}, nil)
	sum := sha256.Sum256(archive)
	configDigest := "sha256:" + hex.EncodeToString(sum[:])
	configPath := filepath.Join(filepath.Dir(fixture.adapter.configPath), strings.TrimPrefix(configDigest, "sha256:"))
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "main.cf"), []byte(contents), configurationMode); err != nil {
		t.Fatal(err)
	}
	statePath := fixture.adapter.statePath
	queuePath := fixture.adapter.definition.mounts[1].source
	helperPath := fixture.adapter.definition.mounts[2].source
	releasePath := fixture.adapter.definition.mounts[3].source
	dockerBinary := fixture.adapter.dockerBinary
	dockerDigest := fixture.adapter.dockerDigest
	if err := fixture.adapter.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPostfix(PostfixOptions{Runtime: RuntimeOptions{DockerBinary: dockerBinary, DockerBinaryDigest: dockerDigest, StateRoot: statePath, ConfigurationRoot: configPath, ContainerPrefix: "gotth", Timeout: 5 * time.Second}, QueueRoot: queuePath, HelperSecretFile: helperPath, ReleaseSecretFile: releasePath})
	if err != nil {
		t.Fatal(err)
	}
	fixture.adapter = reopened
	fixture.adapter.runner = fixture.runner
	fixture.configuration = members
	fixture.configDigest = configDigest
	fixture.configSize = int64(len(archive))
	candidate := fixture.candidate("replace-config", "c", "d")
	prepared, _ := fixture.preflight(candidate)
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){fixture.adapter.StopPrevious, fixture.adapter.RenamePrevious, fixture.adapter.CreateCandidate, fixture.adapter.StartCandidate, fixture.adapter.VerifyCandidate, fixture.adapter.StopCandidate, fixture.adapter.RemoveCandidate, fixture.adapter.RestorePreviousName, fixture.adapter.StartPrevious, fixture.adapter.VerifyPrevious} {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	if fixture.runner.containers["gotth-mail-postfix"].spec.Candidate.ConfigurationDigest != previous.ConfigurationDigest {
		t.Fatal("rollback did not restore previous immutable configuration revision")
	}
}

func TestReplacementSupportsImmutableSecretRevision(t *testing.T) {
	root := t.TempDir()
	contents := "mail_location = maildir:/var/lib/gotth-mail/mail\n"
	archive, members := configurationArchive(t, []archiveInput{{name: "dovecot.conf", contents: contents}}, nil)
	archiveSum := sha256.Sum256(archive)
	configDigest := "sha256:" + hex.EncodeToString(archiveSum[:])
	configStore := testDirectory(t, root, "config-store")
	configPath := filepath.Join(configStore, strings.TrimPrefix(configDigest, "sha256:"))
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "dovecot.conf"), []byte(contents), configurationMode); err != nil {
		t.Fatal(err)
	}
	secretStore := testDirectory(t, root, "secret-store")
	secretPath := func(value string) (string, string) {
		t.Helper()
		sum := sha256.Sum256([]byte(value))
		member := revisionMember{Destination: authSecretTarget, Name: "", Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(value))}
		encoded, err := json.Marshal([]revisionMember{member})
		if err != nil {
			t.Fatal(err)
		}
		revision := digest(encoded)
		directory := filepath.Join(secretStore, strings.TrimPrefix(revision, "sha256:"))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "control-token")
		if err := os.WriteFile(path, []byte(value), 0o400); err != nil {
			t.Fatal(err)
		}
		return revision, path
	}
	previousRevision, previousSecret := secretPath("previous-secret\n")
	candidateRevision, candidateSecret := secretPath("candidate-secret\n")
	state := testDirectory(t, root, "state")
	mailbox := testDirectory(t, root, "mailbox")
	binary := "/usr/bin/true"
	binaryValue, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := OpenDovecot(DovecotOptions{Runtime: RuntimeOptions{DockerBinary: binary, DockerBinaryDigest: digest(binaryValue), StateRoot: state, ConfigurationRoot: configPath, ContainerPrefix: "gotth", Timeout: 5 * time.Second}, MailboxRoot: mailbox, AuthSecretFile: candidateSecret})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	runner := &fakeRuntimeDocker{containers: make(map[string]fakeRuntimeContainer), network: true}
	adapter.adapter.runner = runner
	makeCandidate := func(operation, image, source, revision string) Candidate {
		return Candidate{OperationID: operation, ComponentID: "mail", ProductVersion: "1.0.0-alpha.1", SourceCommit: strings.Repeat(source, 40), Image: roleImageRepositories[RoleDovecot] + "@sha256:" + strings.Repeat(image, 64), ConfigurationDigest: configDigest, ConfigurationSize: int64(len(archive)), ConfigurationMembers: members, SecretRevisionDigest: revision}
	}
	previous := makeCandidate("initial-secret", "a", "b", previousRevision)
	previousSpec := managedSpec{Candidate: previous, Role: RoleDovecot, ImageID: imageDigest(previous.Image), ContainerName: "gotth-mail-dovecot", NetworkName: "gotth-private", MountSources: []string{configPath, mailbox, previousSecret}}
	runner.containers["gotth-mail-dovecot"] = fakeRuntimeContainer{spec: previousSpec, definition: adapter.definition, running: true, id: strings.Repeat("4", 64)}
	candidate := makeCandidate("replace-secret", "c", "d", candidateRevision)
	prepared, _, err := adapter.Preflight(context.Background(), DovecotRequest{Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Stage(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func(context.Context, string) (Summary, error){adapter.StopPrevious, adapter.RenamePrevious, adapter.CreateCandidate, adapter.StartCandidate, adapter.VerifyCandidate, adapter.StopCandidate, adapter.RemoveCandidate, adapter.RestorePreviousName, adapter.StartPrevious, adapter.VerifyPrevious} {
		if _, err := action(context.Background(), candidate.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	if runner.containers["gotth-mail-dovecot"].spec.Candidate.SecretRevisionDigest != previousRevision {
		t.Fatal("rollback did not restore previous immutable secret revision")
	}
}
