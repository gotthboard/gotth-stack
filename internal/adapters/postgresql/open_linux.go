//go:build linux

package postgresql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func Open(options Options) (*Adapter, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < time.Second || timeout > time.Minute || !validPrefix(options.ContainerPrefix) || !digestPattern.MatchString(options.DockerBinaryDigest) {
		return nil, ErrInvalidInput
	}
	for _, path := range []string{options.DockerBinary, options.StateRoot, options.DataRoot, options.SecretRoot} {
		if !cleanAbsolute(path) || strings.ContainsAny(path, "\x00\r\n") {
			return nil, ErrInvalidInput
		}
	}
	if strings.Contains(options.DataRoot, ",") || strings.Contains(options.SecretRoot, ",") {
		return nil, ErrInvalidInput
	}
	if pathsOverlap(options.StateRoot, options.DataRoot) || pathsOverlap(options.StateRoot, options.SecretRoot) || pathsOverlap(options.DataRoot, options.SecretRoot) || pathsOverlap(options.DockerBinary, options.StateRoot) || pathsOverlap(options.DockerBinary, options.DataRoot) || pathsOverlap(options.DockerBinary, options.SecretRoot) {
		return nil, ErrInvalidInput
	}
	if err := validateBinary(options.DockerBinary, options.DockerBinaryDigest); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(filepath.Dir(options.DockerBinary), false); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.StateRoot, true); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.DataRoot, false); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.SecretRoot, false); err != nil {
		return nil, err
	}
	stateRoot, err := os.OpenRoot(options.StateRoot)
	if err != nil {
		return nil, ErrUnsafeState
	}
	dataRoot, err := os.OpenRoot(options.DataRoot)
	if err != nil {
		_ = stateRoot.Close()
		return nil, ErrUnsafeState
	}
	secretRoot, err := os.OpenRoot(options.SecretRoot)
	if err != nil {
		_ = dataRoot.Close()
		_ = stateRoot.Close()
		return nil, ErrUnsafeState
	}
	adapter := &Adapter{
		dockerBinary: options.DockerBinary, dockerDigest: options.DockerBinaryDigest,
		statePath: options.StateRoot, dataPath: options.DataRoot, secretPath: options.SecretRoot,
		prefix: options.ContainerPrefix, timeout: timeout,
		stateRoot: stateRoot, dataRoot: dataRoot, secretRoot: secretRoot, runner: execRunner{},
	}
	failed := true
	defer func() {
		if failed {
			_ = adapter.closeRoots()
		}
	}()
	if err := ensurePrivateDirectory(stateRoot, transactionsName); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(stateRoot, scratchName); err != nil {
		return nil, err
	}
	lock, err := stateRoot.OpenFile("adapter.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, ErrStorage
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 {
		_ = lock.Close()
		return nil, ErrUnsafeState
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, ErrStorage
	}
	adapter.lock = lock
	transactionsRoot, err := stateRoot.OpenRoot(transactionsName)
	if err != nil {
		return nil, ErrUnsafeState
	}
	adapter.transactionsRoot = transactionsRoot
	failed = false
	return adapter, nil
}

func (adapter *Adapter) Close() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil
	}
	adapter.closed = true
	return adapter.closeRoots()
}

func (adapter *Adapter) closeRoots() error {
	var failed bool
	if adapter.transactionsRoot != nil && adapter.transactionsRoot.Close() != nil {
		failed = true
	}
	if adapter.lock != nil {
		if syscall.Flock(int(adapter.lock.Fd()), syscall.LOCK_UN) != nil {
			failed = true
		}
		if adapter.lock.Close() != nil {
			failed = true
		}
	}
	for _, root := range []*os.Root{adapter.secretRoot, adapter.dataRoot, adapter.stateRoot} {
		if root != nil && root.Close() != nil {
			failed = true
		}
	}
	if failed {
		return ErrStorage
	}
	return nil
}

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

func validateBinary(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 || linkCount(info) != 1 {
		return ErrExecutable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return ErrExecutable
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ErrExecutable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() < 1 || opened.Size() > MaxBinaryBytes {
		return ErrExecutable
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, io.LimitReader(file, MaxBinaryBytes+1)); err != nil {
		return ErrStorage
	}
	if "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != expected {
		return ErrExecutable
	}
	return nil
}

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
	} else if info.Mode().Perm()&0o022 != 0 || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) {
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

func (adapter *Adapter) readDataIdentity(spec managedSpec) (fileIdentity, bool, error) {
	info, err := adapter.dataRoot.Lstat(spec.DataName)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fileIdentity{}, false, ErrUnsafeState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != spec.Specification.UID || stat.Gid != spec.Specification.GID {
		return fileIdentity{}, false, ErrUnsafeState
	}
	root, err := adapter.dataRoot.OpenRoot(spec.DataName)
	if err != nil {
		return fileIdentity{}, false, ErrUnsafeState
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fileIdentity{}, false, ErrStorage
	}
	initialized := len(entries) != 0
	if initialized {
		version, err := readBoundedFile(root, "PG_VERSION", 16)
		if err != nil || !bytes.Equal(version, []byte("17\n")) {
			return fileIdentity{}, false, ErrUnsafeState
		}
	}
	return identityFrom(info), initialized, nil
}

func (adapter *Adapter) readSecretIdentity(spec managedSpec) (fileIdentity, error) {
	file, err := adapter.secretRoot.OpenFile(spec.SecretName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fileIdentity{}, ErrSecret
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o007 != 0 || info.Mode().Perm()&0o222 != 0 || linkCount(info) != 1 || info.Size() < 1 || info.Size() > MaxSecretBytes {
		return fileIdentity{}, ErrSecret
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) && stat.Uid != spec.Specification.UID) || !((stat.Uid == spec.Specification.UID && info.Mode().Perm()&0o400 != 0) || (stat.Gid == spec.Specification.GID && info.Mode().Perm()&0o040 != 0)) {
		return fileIdentity{}, ErrSecret
	}
	value, err := io.ReadAll(io.LimitReader(file, MaxSecretBytes+1))
	if err != nil || len(value) > MaxSecretBytes || bytes.IndexByte(value, 0) >= 0 || digest(value) != spec.SecretRevision {
		return fileIdentity{}, ErrSecret
	}
	return identityFrom(info), nil
}

func readBoundedFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafeState
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit || linkCount(info) != 1 {
		return nil, ErrUnsafeState
	}
	value, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(value)) > limit {
		return nil, ErrStorage
	}
	return value, nil
}

func identityFrom(info os.FileInfo) fileIdentity {
	stat := info.Sys().(*syscall.Stat_t)
	return fileIdentity{device: uint64(stat.Dev), inode: stat.Ino, uid: stat.Uid, gid: stat.Gid, mode: uint32(info.Mode().Perm()), size: info.Size()}
}

func ownedByEffectiveUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}
