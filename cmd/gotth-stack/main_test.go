package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotthboard/gotth-stack/pkg/stack"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunValidateAndPlan(t *testing.T) {
	path := writeManifest(t, validCLIManifest())
	for _, command := range []string{"validate", "plan"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{command, path}, &stdout, &stderr); code != 0 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", stderr.String())
			}
			if command == "validate" {
				if !strings.HasPrefix(stdout.String(), "valid sha256:") {
					t.Fatalf("bad validate output: %q", stdout.String())
				}
			} else {
				var plan stack.Plan
				if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || plan.Digest == "" {
					t.Fatalf("bad plan output: %v %q", err, stdout.String())
				}
			}
		})
	}
}

func TestRunRejectsUsageAndManifestWithoutDisclosure(t *testing.T) {
	tests := [][]string{nil, {"unknown", "file"}, {"validate"}, {"plan", "a", "b"}}
	for _, arguments := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(arguments, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", arguments, code, stdout.String(), stderr.String())
		}
	}

	directory := t.TempDir()
	secret := "do-not-disclose-this"
	path := filepath.Join(directory, "bad.json")
	if err := os.WriteFile(path, []byte(`{"secret":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"plan", path}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), path) {
		t.Fatalf("error disclosed input: %q", stderr.String())
	}
}

func TestReadManifestRejectsUnsafeFiles(t *testing.T) {
	directory := t.TempDir()
	regular := writeManifest(t, validCLIManifest())
	symlink := filepath.Join(directory, "manifest-link.json")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(directory, "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), stack.MaxManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", strings.Repeat("x", maxPathBytes+1), directory, symlink, oversized, filepath.Join(directory, "missing")} {
		if _, err := readManifest(path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
}

func TestRunHandlesOutputFailures(t *testing.T) {
	path := writeManifest(t, validCLIManifest())
	for _, command := range []string{"validate", "plan"} {
		var stderr bytes.Buffer
		if code := run([]string{command, path}, failingWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
			t.Fatalf("command=%s code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func writeManifest(t *testing.T, manifest stack.Manifest) string {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validCLIManifest() stack.Manifest {
	digest := "sha256:" + strings.Repeat("a", 64)
	return stack.Manifest{
		SchemaVersion: stack.SchemaVersion,
		Name:          "cli-stack",
		Components: []stack.Component{{
			ID:                  "proxy",
			Adapter:             "gotth-stack-adapter-caddy.v1",
			Artifact:            stack.Artifact{Source: "oci://registry.example.test/caddy", Digest: digest},
			ConfigurationDigest: digest,
		}},
	}
}
