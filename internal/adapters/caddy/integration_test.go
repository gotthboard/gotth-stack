//go:build linux

package caddy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDisposableCaddy(t *testing.T) {
	binary := os.Getenv("GOTTH_CADDY_BIN")
	if binary == "" {
		t.Skip("GOTTH_CADDY_BIN is not set")
	}
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "runtime")
	for _, path := range []string{configDir, stateDir, runtimeDir} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	adminPort := freePort(t)
	sitePort := freePort(t)
	previous := disposableConfig(adminPort, sitePort, "old")
	candidate := disposableConfig(adminPort, sitePort, "new")
	configPath := filepath.Join(configDir, "Caddyfile")
	if err := os.WriteFile(configPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "--config", configPath, "--adapter", "caddyfile")
	command.Dir = root
	command.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + runtimeDir,
		"XDG_CONFIG_HOME=" + filepath.Join(runtimeDir, "config"),
		"XDG_DATA_HOME=" + filepath.Join(runtimeDir, "data"),
	}
	var output limitedBuffer
	output.limit = MaxCommandOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-wait
		}
	})
	adminURL := fmt.Sprintf("http://127.0.0.1:%d", adminPort)
	waitForHTTP(t, adminURL+"/config/")
	adapter, err := Open(Options{
		CaddyBinary: binary, CaddyBinaryDigest: digestFile(t, binary), ConfigPath: configPath, StateRoot: stateDir, WorkingDirectory: root,
		AdminURL: adminURL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("open: %v; caddy output: %s", err, output.value)
	}
	prepared, expected, err := adapter.Preflight(context.Background(), Request{
		OperationID: "disposable-operation", ComponentID: "caddy-main",
		Configuration: candidate, ExpectedDigest: digest(candidate),
	})
	if err != nil {
		t.Fatalf("preflight: %v; caddy output: %s", err, output.value)
	}
	if _, err := adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Install("disposable-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Activate(context.Background(), "disposable-operation"); err != nil {
		t.Fatalf("activate: %v; caddy output: %s", err, output.value)
	}
	if result, err := adapter.Verify(context.Background(), "disposable-operation", DesiredCandidate); err != nil || result != expected {
		t.Fatalf("candidate verify = %#v, %v", result, err)
	}
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", sitePort), "new")
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/stable", sitePort), "stable")
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, err = Open(Options{
		CaddyBinary: binary, CaddyBinaryDigest: digestFile(t, binary), ConfigPath: configPath, StateRoot: stateDir, WorkingDirectory: root,
		AdminURL: adminURL, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if _, err := adapter.Verify(context.Background(), "disposable-operation", DesiredCandidate); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Restore("disposable-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Reactivate(context.Background(), "disposable-operation"); err != nil {
		t.Fatalf("reactivate: %v; caddy output: %s", err, output.value)
	}
	if _, err := adapter.Verify(context.Background(), "disposable-operation", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", sitePort), "old")
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/stable", sitePort), "stable")
	value, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(value, previous) {
		t.Fatalf("restored file mismatch: %v", err)
	}
}

func disposableConfig(adminPort, sitePort int, body string) []byte {
	return []byte(fmt.Sprintf(`{
	admin 127.0.0.1:%d
}

http://127.0.0.1:%d {
	handle /stable {
		respond "stable"
	}
	handle {
		respond %q
	}
}
`, adminPort, sitePort, body))
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
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

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("disposable Caddy did not become ready")
}

func assertBody(t *testing.T, url, want string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	value, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(value) != want {
		t.Fatalf("GET body = %q status %d, want %q", value, response.StatusCode, want)
	}
}
