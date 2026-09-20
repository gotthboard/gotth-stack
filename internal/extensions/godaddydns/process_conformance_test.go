package godaddydns

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gotthboard/gotth-stack/internal/extensions/godaddydns/controlv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestCompiledProviderConformance(t *testing.T) {
	artifactPath := os.Getenv("GOTTH_GODADDY_ARTIFACT")
	if artifactPath == "" {
		t.Skip("GOTTH_GODADDY_ARTIFACT is required")
	}
	artifact, err := os.Open(artifactPath)
	if err != nil {
		t.Fatal("open exact provider artifact")
	}
	defer artifact.Close()
	if _, err := verifyArtifact(artifact, expectedPins); err != nil {
		t.Fatal("exact provider archive verification failed")
	}
	if _, err := artifact.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	request := testRequest()
	admitted, err := VerifyAndAdmit(artifact, request)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	secretDir, runtimeDir := filepath.Join(root, "secrets"), filepath.Join(root, "run")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "gotth-extension-godaddy-dns")
	configPath := filepath.Join(root, "config.json")
	bindingPath := filepath.Join(root, "binding.json")
	tokenPath := filepath.Join(root, "service.token")
	socketPath := filepath.Join(runtimeDir, "provider.sock")
	serviceToken, fakePAT := []byte("12345678901234567890123456789012"), []byte("candidate-conformance-pat-value")
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{{executable, admitted.Executable, 0o700}, {configPath, admitted.ProviderConfiguration, 0o600}, {bindingPath, admitted.RuntimeBinding, 0o600}, {tokenPath, serviceToken, 0o600}, {filepath.Join(secretDir, ExpectedSecretSlot), fakePAT, 0o600}}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(executable)
	command.Env = []string{"GOTTH_EXTENSION_CONFIG_FILE=" + configPath, "GOTTH_EXTENSION_BINDING_FILE=" + bindingPath, "GOTTH_EXTENSION_SECRET_DIR=" + secretDir, "GOTTH_EXTENSION_SERVICE_TOKEN_FILE=" + tokenPath, "GOTTH_EXTENSION_SOCKET=" + socketPath}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal("start exact provider executable")
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	defer func() {
		select {
		case <-done:
			return
		default:
		}
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if info, statErr := os.Lstat(socketPath); statErr == nil {
			if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
				t.Fatalf("socket mode %o", info.Mode().Perm())
			}
			break
		}
		select {
		case <-done:
			t.Fatal("provider exited before socket readiness")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("provider socket readiness timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := grpc.NewClient("passthrough:///godaddy-provider", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := controlv1.NewExtensionControlClient(connection)
	healthRequest := &controlv1.HealthRequest{CorrelationId: "22222222-2222-2222-2222-222222222222", SessionSha256: admitted.SessionSHA256}
	if _, err := client.Health(ctx, healthRequest); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing token result: %v", err)
	}
	wrong := metadata.AppendToOutgoingContext(ctx, "x-gotth-service-token", "wrong-wrong-wrong-wrong-wrong-wrong")
	if _, err := client.Health(wrong, healthRequest); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong token result: %v", err)
	}
	authed := metadata.AppendToOutgoingContext(ctx, "x-gotth-service-token", string(serviceToken))
	challenge := bytes.Repeat([]byte{0x5a}, 32)
	if _, err := client.Handshake(authed, &controlv1.HandshakeRequest{CorrelationId: "33333333-3333-3333-3333-333333333333", Challenge: challenge, ExpectedExtensionId: ExpectedExtensionID, ExpectedManifestSha256: admitted.ManifestSHA256, ExpectedGrantSha256: admitted.GrantSHA256, ExpectedSessionSha256: "0000000000000000000000000000000000000000000000000000000000000000"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mismatched binding result: %v", err)
	}
	handshake, err := client.Handshake(authed, &controlv1.HandshakeRequest{CorrelationId: "33333333-3333-3333-3333-333333333333", Challenge: challenge, ExpectedExtensionId: ExpectedExtensionID, ExpectedManifestSha256: admitted.ManifestSHA256, ExpectedGrantSha256: admitted.GrantSHA256, ExpectedSessionSha256: admitted.SessionSHA256})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(handshake.GetChallenge(), challenge) || handshake.GetExtensionId() != ExpectedExtensionID || handshake.GetExtensionVersion() != ExpectedVersion || handshake.GetManifestSha256() != admitted.ManifestSHA256 || handshake.GetGrantSha256() != admitted.GrantSHA256 || handshake.GetSessionSha256() != admitted.SessionSHA256 || !reflectStrings(handshake.GetCapabilities(), request.Capabilities) {
		t.Fatalf("handshake binding mismatch: %+v", handshake)
	}
	health, err := client.Health(authed, healthRequest)
	if err != nil {
		t.Fatal(err)
	}
	if health.GetState() != controlv1.HealthState_HEALTH_STATE_READY || health.GetCode() != "extension.ready" {
		t.Fatalf("health: %+v", health)
	}
	if bytes.Contains(stdout.Bytes(), serviceToken) || bytes.Contains(stdout.Bytes(), fakePAT) || bytes.Contains(stderr.Bytes(), serviceToken) || bytes.Contains(stderr.Bytes(), fakePAT) {
		t.Fatal("provider output disclosed secret")
	}
}
