//go:build linux

package mailruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func OpenControlPlane(options ControlPlaneOptions) (*ControlPlane, error) {
	mounts := []mountSpec{
		{source: options.DataRoot, destination: controlDataTarget, kind: mountDirectory},
		{source: options.ExtensionStateRoot, destination: extensionStateTarget, kind: mountDirectory},
		{source: options.ExtensionSecretRoot, destination: extensionSecretTarget, readOnly: true, kind: mountDirectory, secret: true, revision: true},
		{source: options.DatabaseSecretFile, destination: databaseSecretTarget, readOnly: true, kind: mountRegularFile, secret: true, revision: true},
		{source: options.MasterSecretFile, destination: masterSecretTarget, readOnly: true, kind: mountRegularFile, secret: true, revision: true},
	}
	value, err := openAdapter(options.Runtime, RoleControlPlane, mounts, nil)
	if err != nil {
		return nil, err
	}
	return &ControlPlane{adapter: value}, nil
}

func OpenFront(options FrontOptions) (*Front, error) {
	mounts := []mountSpec{
		{source: options.CertificateFile, destination: certificateTarget, readOnly: true, kind: mountRegularFile, revision: true},
		{source: options.PrivateKeyFile, destination: privateKeyTarget, readOnly: true, kind: mountRegularFile, secret: true, revision: true},
	}
	ports := []portSpec{{hostIP: "0.0.0.0", hostPort: 25, containerPort: 1025}, {hostIP: "0.0.0.0", hostPort: 465, containerPort: 1465}, {hostIP: "0.0.0.0", hostPort: 587, containerPort: 1587}, {hostIP: "0.0.0.0", hostPort: 143, containerPort: 1143}, {hostIP: "0.0.0.0", hostPort: 993, containerPort: 1993}}
	value, err := openAdapter(options.Runtime, RoleFront, mounts, ports)
	if err != nil {
		return nil, err
	}
	return &Front{adapter: value}, nil
}

func OpenPostfix(options PostfixOptions) (*Postfix, error) {
	value, err := openAdapter(options.Runtime, RolePostfix, []mountSpec{{source: options.QueueRoot, destination: queueTarget, kind: mountDirectory}}, nil)
	if err != nil {
		return nil, err
	}
	return &Postfix{adapter: value}, nil
}

func OpenDovecot(options DovecotOptions) (*Dovecot, error) {
	mounts := []mountSpec{{source: options.MailboxRoot, destination: mailboxTarget, kind: mountDirectory}, {source: options.AuthSecretFile, destination: authSecretTarget, readOnly: true, kind: mountRegularFile, secret: true, revision: true}}
	value, err := openAdapter(options.Runtime, RoleDovecot, mounts, nil)
	if err != nil {
		return nil, err
	}
	return &Dovecot{adapter: value}, nil
}

func OpenRspamd(options RspamdOptions) (*Rspamd, error) {
	mounts := []mountSpec{{source: options.DataRoot, destination: rspamdStateTarget, kind: mountDirectory}, {source: options.DKIMRoot, destination: dkimTarget, readOnly: true, kind: mountDirectory, secret: true, revision: true}, {source: options.ControllerSecretFile, destination: rspamdSecretTarget, readOnly: true, kind: mountRegularFile, secret: true, revision: true}}
	value, err := openAdapter(options.Runtime, RoleRspamd, mounts, nil)
	if err != nil {
		return nil, err
	}
	return &Rspamd{adapter: value}, nil
}

func openAdapter(options RuntimeOptions, role Role, mounts []mountSpec, ports []portSpec) (*adapter, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < time.Second || timeout > time.Minute || !validPrefix(options.ContainerPrefix) || !digestPattern.MatchString(options.DockerBinaryDigest) || roleImageRepositories[role] == "" {
		return nil, ErrInvalidInput
	}
	paths := []string{options.DockerBinary, options.StateRoot, options.ConfigurationRoot}
	for _, mount := range mounts {
		paths = append(paths, mount.source)
	}
	for _, path := range paths {
		if !cleanAbsolute(path) || strings.ContainsAny(path, "\x00\r\n,") {
			return nil, ErrInvalidInput
		}
	}
	for left := range paths {
		for right := left + 1; right < len(paths); right++ {
			if pathsOverlap(paths[left], paths[right]) {
				return nil, ErrInvalidInput
			}
		}
	}
	if err := validateBinary(options.DockerBinary, options.DockerBinaryDigest); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(filepath.Dir(options.DockerBinary), false); err != nil {
		return nil, ErrExecutable
	}
	if err := validateDirectoryPath(options.StateRoot, true); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.ConfigurationRoot, false); err != nil {
		return nil, ErrConfiguration
	}
	definition := roleDefinition{role: role, adapterID: "mailruntime." + string(role) + ".v1", repository: roleImageRepositories[role], alias: string(role), user: "1000:1000", ports: append([]portSpec(nil), ports...)}
	if role == RolePostfix || role == RoleDovecot {
		definition.user = "0:0"
		definition.capAdd = []string{"CHOWN", "DAC_OVERRIDE", "DAC_READ_SEARCH", "FOWNER", "SETGID", "SETUID"}
	}
	definition.mounts = append(definition.mounts, mountSpec{source: options.ConfigurationRoot, destination: configTarget, readOnly: true, kind: mountDirectory})
	definition.mounts = append(definition.mounts, mounts...)
	value := &adapter{dockerBinary: options.DockerBinary, dockerDigest: options.DockerBinaryDigest, statePath: options.StateRoot, configPath: options.ConfigurationRoot, prefix: options.ContainerPrefix, network: options.ContainerPrefix + "-private", timeout: timeout, definition: definition, runner: execRunner{}}
	failed := true
	defer func() {
		if failed {
			_ = value.close()
		}
	}()
	stateRoot, err := os.OpenRoot(options.StateRoot)
	if err != nil {
		return nil, ErrUnsafeState
	}
	value.stateRoot = stateRoot
	if err := ensurePrivateDirectory(stateRoot, "transactions"); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(stateRoot, "scratch"); err != nil {
		return nil, err
	}
	lock, err := stateRoot.OpenFile("adapter.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, ErrStorage
	}
	value.lock = lock
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 {
		return nil, ErrUnsafeState
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, ErrStorage
	}
	transactions, err := stateRoot.OpenRoot("transactions")
	if err != nil {
		return nil, ErrUnsafeState
	}
	value.transactions = transactions
	for _, mount := range definition.mounts {
		file, err := openMount(mount)
		if err != nil {
			return nil, err
		}
		value.mountFiles = append(value.mountFiles, file)
	}
	failed = false
	return value, nil
}

func openMount(mount mountSpec) (*os.File, error) {
	flags := os.O_RDONLY | syscall.O_NOFOLLOW
	if mount.kind == mountDirectory {
		flags |= syscall.O_DIRECTORY
	}
	file, err := os.OpenFile(mount.source, flags, 0)
	if err != nil {
		if mount.secret {
			return nil, ErrSecret
		}
		return nil, ErrUnsafeState
	}
	info, err := file.Stat()
	wanted := info != nil && ((mount.kind == mountDirectory && info.IsDir()) || (mount.kind == mountRegularFile && info.Mode().IsRegular() && linkCount(info) == 1))
	stat, statOK := any(nil).(*syscall.Stat_t)
	if info != nil {
		stat, statOK = info.Sys().(*syscall.Stat_t)
	}
	if err != nil || !wanted || !statOK || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) && stat.Uid != 1000) || info.Mode().Perm()&0o022 != 0 || mount.secret && (info.Mode().Perm()&0o007 != 0 || info.Size() > 1<<20) {
		_ = file.Close()
		if mount.secret {
			return nil, ErrSecret
		}
		return nil, ErrUnsafeState
	}
	return file, nil
}

func (adapter *adapter) close() error {
	failed := false
	for _, file := range adapter.mountFiles {
		if file != nil && file.Close() != nil {
			failed = true
		}
	}
	if adapter.transactions != nil && adapter.transactions.Close() != nil {
		failed = true
	}
	if adapter.lock != nil {
		if syscall.Flock(int(adapter.lock.Fd()), syscall.LOCK_UN) != nil || adapter.lock.Close() != nil {
			failed = true
		}
	}
	if adapter.stateRoot != nil && adapter.stateRoot.Close() != nil {
		failed = true
	}
	if failed {
		return ErrStorage
	}
	return nil
}

func (adapter *adapter) Close() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil
	}
	adapter.closed = true
	return adapter.close()
}

func (value *ControlPlane) Close() error { return value.adapter.Close() }
func (value *Front) Close() error        { return value.adapter.Close() }
func (value *Postfix) Close() error      { return value.adapter.Close() }
func (value *Dovecot) Close() error      { return value.adapter.Close() }
func (value *Rspamd) Close() error       { return value.adapter.Close() }

func cleanAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func pathsOverlap(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	return left == right || isWithin(left, right) || isWithin(right, left)
}

func isWithin(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validPrefix(value string) bool { return len(value) <= 24 && validID(value) }

func validateDirectoryPath(path string, private bool) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return ErrUnsafeState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrUnsafeState
	}
	if private {
		if info.Mode().Perm() != 0o700 || stat.Uid != uint32(os.Geteuid()) {
			return ErrUnsafeState
		}
	} else if info.Mode().Perm()&0o022 != 0 || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) && stat.Uid != 1000) {
		return ErrUnsafeState
	}
	return nil
}

func ensurePrivateDirectory(root *os.Root, name string) error {
	if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return ErrStorage
	}
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return ErrUnsafeState
	}
	return nil
}

func ownedByEffectiveUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
