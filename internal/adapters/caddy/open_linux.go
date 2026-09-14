//go:build linux

package caddy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func Open(options Options) (*Adapter, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < time.Second || timeout > time.Minute {
		return nil, ErrInvalidInput
	}
	if !digestPattern.MatchString(options.CaddyBinaryDigest) {
		return nil, ErrInvalidInput
	}
	if !cleanAbsolute(options.CaddyBinary) || !cleanAbsolute(options.ConfigPath) || !cleanAbsolute(options.StateRoot) || !cleanAbsolute(options.WorkingDirectory) {
		return nil, ErrInvalidInput
	}
	if pathsOverlap(options.ConfigPath, options.StateRoot) || pathsOverlap(options.CaddyBinary, options.StateRoot) || options.CaddyBinary == options.ConfigPath {
		return nil, ErrInvalidInput
	}
	adminURL, adminAddress, err := validateAdminURL(options.AdminURL)
	if err != nil {
		return nil, err
	}
	if err := validateBinary(options.CaddyBinary, options.CaddyBinaryDigest); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(filepath.Dir(options.CaddyBinary), false); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(filepath.Dir(options.ConfigPath), false); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.StateRoot, true); err != nil {
		return nil, err
	}
	if err := validateDirectoryPath(options.WorkingDirectory, false); err != nil {
		return nil, err
	}
	configRoot, err := os.OpenRoot(filepath.Dir(options.ConfigPath))
	if err != nil {
		return nil, ErrUnsafeState
	}
	stateRoot, err := os.OpenRoot(options.StateRoot)
	if err != nil {
		_ = configRoot.Close()
		return nil, ErrUnsafeState
	}
	if err := validateOpenedDirectory(configRoot, false); err != nil {
		_ = stateRoot.Close()
		_ = configRoot.Close()
		return nil, err
	}
	if err := validateOpenedDirectory(stateRoot, true); err != nil {
		_ = stateRoot.Close()
		_ = configRoot.Close()
		return nil, err
	}
	adapter := &Adapter{
		binary: options.CaddyBinary, binaryDigest: options.CaddyBinaryDigest, configPath: options.ConfigPath,
		configName: filepath.Base(options.ConfigPath), statePath: options.StateRoot,
		adminURL: adminURL, adminAddress: adminAddress, workingDirectory: options.WorkingDirectory, timeout: timeout,
		configRoot: configRoot, stateRoot: stateRoot, runner: execRunner{}, ops: defaultStorageOps(),
	}
	failed := true
	defer func() {
		if failed {
			_ = adapter.closeRoots()
		}
	}()
	if _, _, err := adapter.readTarget(); err != nil {
		return nil, err
	}
	for _, name := range []string{transactionDirName, scratchDirName} {
		if err := ensurePrivateDirectory(stateRoot, name); err != nil {
			return nil, err
		}
	}
	lock, err := openLock(stateRoot)
	if err != nil {
		return nil, err
	}
	adapter.lock = lock
	transactionsRoot, err := stateRoot.OpenRoot(transactionDirName)
	if err != nil {
		return nil, ErrUnsafeState
	}
	adapter.transactionsRoot = transactionsRoot
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: -1}).DialContext,
		DisableKeepAlives:     true,
		DisableCompression:    true,
		MaxIdleConns:          1,
		ResponseHeaderTimeout: timeout,
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	adapter.runtime = &adminReader{url: adminURL + "/config/", client: client}
	failed = false
	return adapter, nil
}

func cleanAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return left == right || isWithin(left, right) || isWithin(right, left)
}

func isWithin(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateAdminURL(value string) (string, string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", ErrInvalidInput
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", "", ErrInvalidInput
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portText)
	if err != nil || ip == nil || !ip.IsLoopback() || port < 1 || port > 65535 {
		return "", "", ErrInvalidInput
	}
	canonicalHost := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	canonicalURL := "http://" + canonicalHost
	if value != canonicalURL {
		return "", "", ErrInvalidInput
	}
	return canonicalURL, canonicalHost, nil
}

func validateBinary(path, expectedDigest string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 || linkCount(info) != 1 {
		return ErrUnsafeState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) {
		return ErrUnsafeState
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ErrUnsafeState
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrUnsafeState
	}
	if opened.Size() < 1 || opened.Size() > MaxBinaryBytes {
		return ErrLimit
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, io.LimitReader(file, MaxBinaryBytes+1)); err != nil {
		return ErrStorage
	}
	actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if actual != expectedDigest {
		return ErrConflict
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

func validateOpenedDirectory(root *os.Root, private bool) error {
	directory, err := root.Open(".")
	if err != nil {
		return ErrUnsafeState
	}
	defer directory.Close()
	info, err := directory.Stat()
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
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir(name, 0o700); err != nil {
			return ErrStorage
		}
		if err := syncRoot(root); err != nil {
			return ErrStorage
		}
		info, err = root.Lstat(name)
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return ErrUnsafeState
	}
	return nil
}

func openLock(root *os.Root) (*os.File, error) {
	const name = "adapter.lock"
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if createErr != nil {
			return nil, ErrStorage
		}
		if syncErr := file.Sync(); syncErr != nil {
			_ = file.Close()
			return nil, ErrStorage
		}
		if closeErr := file.Close(); closeErr != nil {
			return nil, ErrStorage
		}
		if syncErr := syncRoot(root); syncErr != nil {
			return nil, ErrStorage
		}
		info, err = root.Lstat(name)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || linkCount(info) != 1 {
		return nil, ErrUnsafeState
	}
	file, err := root.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafeState
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, ErrUnsafeState
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, ErrStorage
	}
	return file, nil
}

func (adapter *Adapter) readTarget() ([]byte, fileIdentity, error) {
	value, identity, err := adapter.readTargetRaw()
	if err != nil {
		return nil, fileIdentity{}, err
	}
	if err := validateConfiguration(value); err != nil {
		return nil, fileIdentity{}, err
	}
	return value, identity, nil
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

func safeConfigMode(mode os.FileMode) bool {
	permissions := mode.Perm()
	return permissions&0o400 != 0 && permissions&0o111 == 0 && permissions&0o022 == 0
}

func defaultStorageOps() storageOps {
	return storageOps{
		chown: func(file *os.File, uid, gid int) error { return file.Chown(uid, gid) },
		chmod: func(file *os.File, mode os.FileMode) error { return file.Chmod(mode) },
		write: func(file *os.File, value []byte) error {
			for len(value) > 0 {
				written, err := file.Write(value)
				if err != nil {
					return err
				}
				if written == 0 {
					return io.ErrShortWrite
				}
				value = value[written:]
			}
			return nil
		},
		sync:     func(file *os.File) error { return file.Sync() },
		close:    func(file *os.File) error { return file.Close() },
		rename:   func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) },
		syncRoot: syncRoot,
	}
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (adapter *Adapter) Close() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return ErrClosed
	}
	adapter.closed = true
	if err := adapter.closeRoots(); err != nil {
		return ErrStorage
	}
	return nil
}

func (adapter *Adapter) closeRoots() error {
	var result error
	if adapter.lock != nil {
		result = errors.Join(result, adapter.lock.Close())
		adapter.lock = nil
	}
	for _, root := range []*os.Root{adapter.transactionsRoot, adapter.stateRoot, adapter.configRoot} {
		if root != nil {
			result = errors.Join(result, root.Close())
		}
	}
	return result
}
