//go:build linux

package mailruntime

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDisposableRspamdReplacement(t *testing.T) {
	binary := os.Getenv("GOTTH_MAIL_DOCKER_BIN")
	image := os.Getenv("GOTTH_MAIL_RSPAMD_IMAGE")
	manifestPath := os.Getenv("GOTTH_MAIL_RELEASE_MANIFEST")
	archivePath := os.Getenv("GOTTH_MAIL_CONFIGURATION_ARCHIVE")
	sourceCommit := os.Getenv("GOTTH_MAIL_SOURCE_COMMIT")
	if binary == "" || image == "" || manifestPath == "" || archivePath == "" || sourceCommit == "" {
		t.Skip("set GOTTH_MAIL_DOCKER_BIN, GOTTH_MAIL_RSPAMD_IMAGE, GOTTH_MAIL_RELEASE_MANIFEST, GOTTH_MAIL_CONFIGURATION_ARCHIVE, and GOTTH_MAIL_SOURCE_COMMIT")
	}
	if os.Geteuid() != 0 {
		t.Skip("disposable Mail container proof requires root")
	}
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseReleaseManifest(manifestFile)
	if closeErr := manifestFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	err = VerifyConfigurationArchive(archiveFile, parsed.Manifest)
	if closeErr := archiveFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	role, ok := findRole(parsed.Manifest.Roles, RoleRspamd)
	if !ok || role.Image != image || parsed.Manifest.SourceCommit != sourceCommit {
		t.Fatal("release manifest does not bind the requested Rspamd candidate")
	}

	root, err := os.MkdirTemp("", "gotth-mail-rspamd-proof.*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	prefix := "gotthmailproof" + strconv.Itoa(os.Getpid())
	primary := prefix + "-mail-rspamd"
	network := prefix + "-private"
	operations := []string{"fresh-install", "secret-replacement"}
	cleanup := func() {
		for _, operation := range operations {
			name := primary + "-rollback-" + strings.TrimPrefix(digest([]byte(operation)), "sha256:")[:12]
			_ = disposableDocker(binary, "rm", "--force", name)
		}
		_ = disposableDocker(binary, "rm", "--force", primary)
		_ = disposableDocker(binary, "network", "rm", network)
	}
	cleanup()
	t.Cleanup(cleanup)

	configStore := filepath.Join(root, "configuration")
	configRoot := filepath.Join(configStore, strings.TrimPrefix(parsed.Manifest.ConfigurationArchive.Digest, "sha256:"))
	if err := extractVerifiedConfiguration(archivePath, configRoot); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(root, "state")
	dataRoot := filepath.Join(root, "rspamd")
	secretStore := filepath.Join(root, "secrets")
	for _, path := range []string{stateRoot, dataRoot, secretStore} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chown(dataRoot, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	firstRevision, firstDKIM, firstToken := writeRspamdRevision(t, secretStore, "first_rspamd_controller_token_0123456789")
	secondRevision, secondDKIM, secondToken := writeRspamdRevision(t, secretStore, "second_rspamd_controller_token_012345678")
	dockerDigest := disposableFileDigest(t, binary)
	options := func(dkim, token string) RspamdOptions {
		return RspamdOptions{
			Runtime:  RuntimeOptions{DockerBinary: binary, DockerBinaryDigest: dockerDigest, StateRoot: stateRoot, ConfigurationRoot: configRoot, ContainerPrefix: prefix, Timeout: time.Minute},
			DataRoot: dataRoot, DKIMRoot: dkim, ControllerSecretFile: token,
		}
	}
	candidate := func(operation, revision string) Candidate {
		return Candidate{
			OperationID: operation, ComponentID: "mail", ProductVersion: parsed.Manifest.ProductVersion,
			SourceCommit: sourceCommit, Image: image,
			ConfigurationDigest: parsed.Manifest.ConfigurationArchive.Digest, ConfigurationSize: parsed.Manifest.ConfigurationArchive.Size,
			ConfigurationMembers: append([]FileArtifact(nil), parsed.Manifest.ConfigurationMembers...), SecretRevisionDigest: revision,
		}
	}

	adapter, err := OpenRspamd(options(firstDKIM, firstToken))
	if err != nil {
		t.Fatal(err)
	}
	if err := disposableDocker(binary, "network", "create", "--driver", "bridge", "--label", "com.gotthstack.network=mailruntime.v1", network); err != nil {
		_ = adapter.Close()
		t.Fatal(err)
	}
	first := candidate(operations[0], firstRevision)
	prepared, summary, err := adapter.Preflight(context.Background(), RspamdRequest{Candidate: first})
	if err != nil || summary.HadPrevious {
		_ = adapter.Close()
		t.Fatalf("fresh preflight: summary=%#v error=%v", summary, err)
	}
	if _, err := prepared.Stage(); err != nil {
		_ = adapter.Close()
		t.Fatal(err)
	}
	for _, action := range []struct {
		name string
		run  func(context.Context, string) (Summary, error)
	}{{"stop previous", adapter.StopPrevious}, {"rename previous", adapter.RenamePrevious}, {"create candidate", adapter.CreateCandidate}, {"start candidate", adapter.StartCandidate}, {"verify candidate", adapter.VerifyCandidate}, {"commit candidate", adapter.CommitCandidate}} {
		if _, err := action.run(context.Background(), first.OperationID); err != nil {
			_ = adapter.Close()
			t.Fatalf("%s: %v", action.name, err)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	adapter, err = OpenRspamd(options(secondDKIM, secondToken))
	if err != nil {
		t.Fatal(err)
	}
	second := candidate(operations[1], secondRevision)
	prepared, summary, err = adapter.Preflight(context.Background(), RspamdRequest{Candidate: second})
	if err != nil || !summary.HadPrevious || !summary.PreviousRunning {
		_ = adapter.Close()
		t.Fatalf("replacement preflight: summary=%#v error=%v", summary, err)
	}
	if _, err := prepared.Stage(); err != nil {
		_ = adapter.Close()
		t.Fatal(err)
	}
	for _, action := range []struct {
		name string
		run  func(context.Context, string) (Summary, error)
	}{{"stop previous", adapter.StopPrevious}, {"rename previous", adapter.RenamePrevious}, {"create candidate", adapter.CreateCandidate}, {"start candidate", adapter.StartCandidate}, {"verify candidate", adapter.VerifyCandidate}} {
		if _, err := action.run(context.Background(), second.OperationID); err != nil {
			_ = adapter.Close()
			t.Fatalf("%s: %v", action.name, err)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	adapter, err = OpenRspamd(options(secondDKIM, secondToken))
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if _, err := adapter.VerifyCandidate(context.Background(), second.OperationID); err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		name string
		run  func(context.Context, string) (Summary, error)
	}{{"stop candidate", adapter.StopCandidate}, {"remove candidate", adapter.RemoveCandidate}, {"restore previous", adapter.RestorePreviousName}, {"start previous", adapter.StartPrevious}, {"verify previous", adapter.VerifyPrevious}} {
		if _, err := action.run(context.Background(), second.OperationID); err != nil {
			t.Fatalf("%s: %v", action.name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "bayes.ham.sqlite3")); err != nil {
		t.Fatalf("durable Rspamd state was not preserved: %v", err)
	}
}

func findRole(roles []RoleArtifact, wanted Role) (RoleArtifact, bool) {
	for _, role := range roles {
		if role.Role == wanted {
			return role, true
		}
	}
	return RoleArtifact{}, false
}

func extractVerifiedConfiguration(archivePath, root string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	if err := os.Chown(root, 0, 1000); err != nil {
		return err
	}
	handle, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer handle.Close()
	stream := tar.NewReader(handle)
	for {
		header, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(header.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := os.Chown(filepath.Dir(path), 0, 1000); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, configurationMode)
		if err != nil {
			return err
		}
		written, copyErr := io.CopyN(file, stream, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			return errors.New("extract verified configuration")
		}
		if err := os.Chown(path, 0, 1000); err != nil {
			return err
		}
	}
}

func writeRspamdRevision(t *testing.T, store, token string) (string, string, string) {
	t.Helper()
	value := []byte(token)
	sum := sha256.Sum256(value)
	members := []revisionMember{{Destination: rspamdSecretTarget, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(value))}}
	sort.Slice(members, func(left, right int) bool {
		if members[left].Destination != members[right].Destination {
			return members[left].Destination < members[right].Destination
		}
		return members[left].Name < members[right].Name
	})
	encoded, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	revision := digest(encoded)
	root := filepath.Join(store, strings.TrimPrefix(revision, "sha256:"))
	dkim := filepath.Join(root, "dkim")
	if err := os.MkdirAll(dkim, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 0, 1000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dkim, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "controller-token")
	if err := os.WriteFile(path, value, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	return revision, dkim, path
}

func disposableFileDigest(t *testing.T, path string) string {
	t.Helper()
	handle, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, handle); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

func disposableDocker(binary string, arguments ...string) error {
	command := exec.Command(binary, append([]string{"--host", dockerHost}, arguments...)...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "DOCKER_CONFIG=/tmp"}
	return command.Run()
}
