//go:build linux

package authentik

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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
	if timeout < time.Second || timeout > time.Minute || !validID(options.ContainerPrefix) || !digestPattern.MatchString(options.DockerBinaryDigest) {
		return nil, ErrInvalidInput
	}
	for _, path := range []string{options.DockerBinary, options.StateRoot, options.DataRoot, options.SecretRoot} {
		if !cleanAbsolute(path) || strings.ContainsAny(path, "\x00\r\n,") {
			return nil, ErrInvalidInput
		}
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
		stateRoot.Close()
		return nil, ErrUnsafeState
	}
	secretRoot, err := os.OpenRoot(options.SecretRoot)
	if err != nil {
		dataRoot.Close()
		stateRoot.Close()
		return nil, ErrUnsafeState
	}
	adapter := &Adapter{dockerBinary: options.DockerBinary, dockerDigest: options.DockerBinaryDigest, statePath: options.StateRoot, dataPath: options.DataRoot, secretPath: options.SecretRoot, prefix: options.ContainerPrefix, timeout: timeout, stateRoot: stateRoot, dataRoot: dataRoot, secretRoot: secretRoot, runner: execRunner{}}
	failed := true
	defer func() {
		if failed {
			_ = adapter.closeRoots()
		}
	}()
	for _, name := range []string{transactionsName, scratchName} {
		if err := ensurePrivateDirectory(stateRoot, name); err != nil {
			return nil, err
		}
	}
	lock, err := stateRoot.OpenFile("adapter.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, ErrStorage
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 {
		lock.Close()
		return nil, ErrUnsafeState
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
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
	failed := false
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
	} else if info.Mode().Perm()&0o022 != 0 || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
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
