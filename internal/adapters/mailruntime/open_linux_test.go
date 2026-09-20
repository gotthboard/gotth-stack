//go:build linux

package mailruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testRuntimeOptions(t *testing.T, root, role string) RuntimeOptions {
	t.Helper()
	state := filepath.Join(root, role+"-state")
	config := filepath.Join(root, role+"-config")
	for _, path := range []string{state, config} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	binary := "/usr/bin/true"
	value, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return RuntimeOptions{DockerBinary: binary, DockerBinaryDigest: digest(value), StateRoot: state, ConfigurationRoot: config, ContainerPrefix: "gotth"}
}

func testDirectory(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testSecret(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("value\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenAllTypedRoles(t *testing.T) {
	root := t.TempDir()
	control, err := OpenControlPlane(ControlPlaneOptions{
		Runtime: testRuntimeOptions(t, root, "control"), DataRoot: testDirectory(t, root, "control-data"),
		ExtensionStateRoot: testDirectory(t, root, "extension-state"), ExtensionSecretRoot: testDirectory(t, root, "extension-secrets"),
		DatabaseSecretFile: testSecret(t, root, "database-password"), MasterSecretFile: testSecret(t, root, "master-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	front, err := OpenFront(FrontOptions{Runtime: testRuntimeOptions(t, root, "front"), CertificateFile: testSecret(t, root, "certificate"), PrivateKeyFile: testSecret(t, root, "private-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	postfix, err := OpenPostfix(PostfixOptions{Runtime: testRuntimeOptions(t, root, "postfix"), QueueRoot: testDirectory(t, root, "queue")})
	if err != nil {
		t.Fatal(err)
	}
	defer postfix.Close()
	dovecot, err := OpenDovecot(DovecotOptions{Runtime: testRuntimeOptions(t, root, "dovecot"), MailboxRoot: testDirectory(t, root, "mailbox"), AuthSecretFile: testSecret(t, root, "auth-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer dovecot.Close()
	rspamd, err := OpenRspamd(RspamdOptions{Runtime: testRuntimeOptions(t, root, "rspamd"), DataRoot: testDirectory(t, root, "rspamd-data"), DKIMRoot: testDirectory(t, root, "dkim"), ControllerSecretFile: testSecret(t, root, "rspamd-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer rspamd.Close()
	if control.definition.role != RoleControlPlane || front.definition.role != RoleFront || postfix.definition.role != RolePostfix || dovecot.definition.role != RoleDovecot || rspamd.definition.role != RoleRspamd {
		t.Fatal("typed constructor returned wrong role")
	}
	if len(front.definition.ports) != 5 || len(control.definition.ports)+len(postfix.definition.ports)+len(dovecot.definition.ports)+len(rspamd.definition.ports) != 0 {
		t.Fatal("public listener ownership is not exclusive to front")
	}
}

func TestOpenRejectsUnsafeBoundaries(t *testing.T) {
	root := t.TempDir()
	options := testRuntimeOptions(t, root, "postfix")
	queue := testDirectory(t, root, "queue")
	first, err := OpenPostfix(PostfixOptions{Runtime: options, QueueRoot: queue})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := OpenPostfix(PostfixOptions{Runtime: options, QueueRoot: queue}); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open error=%v", err)
	}
	overlap := testRuntimeOptions(t, root, "overlap")
	overlap.ConfigurationRoot = queue
	if _, err := OpenPostfix(PostfixOptions{Runtime: overlap, QueueRoot: queue}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("overlap error=%v", err)
	}
	unsafe := testRuntimeOptions(t, root, "unsafe")
	unsafe.StateRoot = filepath.Join(root, "unsafe-state-link")
	if err := os.Symlink(options.StateRoot, unsafe.StateRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPostfix(PostfixOptions{Runtime: unsafe, QueueRoot: testDirectory(t, root, "unsafe-queue")}); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("symlink state error=%v", err)
	}
}
