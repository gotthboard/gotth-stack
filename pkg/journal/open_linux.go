//go:build linux

package journal

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var requiredFiles = map[string]struct{}{
	"installation.json": {}, "journal.log": {}, "journal.head": {}, "journal.lock": {},
}

// Create establishes the private directory and all identity/checkpoint files
// before reopening through the same validation and lock path used on restart.
func Create(path, installationID string) (*Journal, error) {
	if path == "" || !validID(installationID) {
		return nil, ErrInvalidInput
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return nil, ErrStorage
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrStorage
	}
	if err := validateOpenRoot(root); err != nil {
		_ = root.Close()
		_ = os.RemoveAll(path)
		return nil, err
	}
	failed := true
	defer func() {
		_ = root.Close()
		if failed {
			_ = os.RemoveAll(path)
		}
	}()
	installation := installationFile{SchemaVersion: SchemaVersion, InstallationID: installationID}
	if err := createJSONFile(root, "installation.json", installation); err != nil {
		return nil, err
	}
	if err := createEmptyFile(root, "journal.log"); err != nil {
		return nil, err
	}
	if err := createJSONFile(root, "journal.head", headFile{SchemaVersion: SchemaVersion, Digest: zeroDigest}); err != nil {
		return nil, err
	}
	if err := createEmptyFile(root, "journal.lock"); err != nil {
		return nil, err
	}
	if err := syncRoot(root); err != nil {
		return nil, ErrStorage
	}
	if err := syncParent(path); err != nil {
		return nil, ErrStorage
	}
	_ = root.Close()
	journal, _, err := Open(path, installationID)
	if err != nil {
		return nil, err
	}
	failed = false
	return journal, nil
}

// Open validates every filesystem object before taking the lifetime lock and
// replaying the log. Unexpected names and broad modes are rejected rather
// than repaired, except for the untrusted fixed head temporary file.
func Open(path, installationID string) (*Journal, Recovery, error) {
	if path == "" || !validID(installationID) {
		return nil, Recovery{}, ErrInvalidInput
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return nil, Recovery{}, ErrCorrupt
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, Recovery{}, ErrStorage
	}
	if err := validateOpenRoot(root); err != nil {
		_ = root.Close()
		return nil, Recovery{}, err
	}
	journal := &Journal{
		root: root, installationID: installationID, now: time.Now, ops: defaultStorageOps(),
		approvals: make(map[string]Approval), operations: make(map[string]*Operation),
	}
	failed := true
	defer func() {
		if failed {
			_ = journal.closeFiles()
		}
	}()
	if err := validateRegularFile(root, "journal.lock"); err != nil {
		return nil, Recovery{}, err
	}
	lock, err := root.OpenFile("journal.lock", os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, Recovery{}, ErrCorrupt
	}
	journal.lock = lock
	if err := validateOpenRegularFile(lock); err != nil {
		return nil, Recovery{}, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, Recovery{}, ErrLocked
		}
		return nil, Recovery{}, ErrStorage
	}
	if err := validateDirectory(root); err != nil {
		return nil, Recovery{}, err
	}
	if err := validateInstallation(root, installationID); err != nil {
		return nil, Recovery{}, err
	}
	if err := loadHead(root, &journal.head); err != nil {
		return nil, Recovery{}, err
	}
	log, err := root.OpenFile("journal.log", os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, Recovery{}, ErrCorrupt
	}
	journal.log = log
	if err := validateOpenRegularFile(log); err != nil {
		return nil, Recovery{}, err
	}
	recovery, err := journal.replay()
	if err != nil {
		return nil, Recovery{}, err
	}
	failed = false
	return journal, recovery, nil
}

func createJSONFile(root *os.Root, name string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ErrStorage
	}
	encoded = append(encoded, '\n')
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := defaultStorageOps().write(file, encoded); err != nil {
		return ErrStorage
	}
	if err := file.Sync(); err != nil {
		return ErrStorage
	}
	closed = true
	if err := file.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

func createEmptyFile(root *os.Root, name string) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrStorage
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := file.Sync(); err != nil {
		return ErrStorage
	}
	closed = true
	if err := file.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

func syncParent(path string) error {
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

// validateDirectory admits only the four fixed files. A stale fixed head
// temporary file is untrusted crash debris and is removed after the lock.
func validateDirectory(root *os.Root) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return ErrCorrupt
	}
	seen := make(map[string]struct{}, len(entries))
	removedTemporary := false
	for _, entry := range entries {
		name := entry.Name()
		if name == "journal.head.tmp" {
			info, err := root.Lstat(name)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) {
				return ErrCorrupt
			}
			if err := root.Remove(name); err != nil {
				return ErrStorage
			}
			removedTemporary = true
			continue
		}
		if _, ok := requiredFiles[name]; !ok {
			return ErrCorrupt
		}
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) {
			return ErrCorrupt
		}
		seen[name] = struct{}{}
	}
	if len(seen) != len(requiredFiles) {
		return ErrCorrupt
	}
	if removedTemporary {
		if err := syncRoot(root); err != nil {
			return ErrStorage
		}
	}
	return nil
}

func validateRegularFile(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) {
		return ErrCorrupt
	}
	return nil
}

// validateOpenRoot checks the directory descriptor rather than trusting the
// path-level Lstat result across a possible replacement race.
func validateOpenRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return ErrCorrupt
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByEffectiveUser(info) {
		return ErrCorrupt
	}
	return nil
}

// validateOpenRegularFile closes the Lstat/Open race by checking the object
// actually held by the descriptor before it can be trusted as journal state.
func validateOpenRegularFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) {
		return ErrCorrupt
	}
	return nil
}

func validateInstallation(root *os.Root, expected string) error {
	raw, err := readFixedFile(root, "installation.json", 4096)
	if err != nil || len(raw) > 4096 {
		return ErrCorrupt
	}
	var installation installationFile
	if err := decodeCanonicalDocument(raw, &installation); err != nil {
		return err
	}
	if installation.SchemaVersion != SchemaVersion || installation.InstallationID != expected {
		return ErrCorrupt
	}
	return nil
}

func loadHead(root *os.Root, head *headFile) error {
	raw, err := readFixedFile(root, "journal.head", 4096)
	if err != nil || len(raw) > 4096 {
		return ErrCorrupt
	}
	if err := decodeCanonicalDocument(raw, head); err != nil {
		return err
	}
	if head.SchemaVersion != SchemaVersion || (head.Sequence == 0 && head.Digest != zeroDigest) || (head.Sequence > 0 && !validDigest(head.Digest)) {
		return ErrCorrupt
	}
	return nil
}

// readFixedFile combines O_NOFOLLOW with descriptor-level type, mode, and size
// checks so validation is not undone by following an internal symlink.
func readFixedFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByEffectiveUser(info) || info.Size() > limit {
		return nil, ErrCorrupt
	}
	return io.ReadAll(io.LimitReader(file, limit+1))
}

func ownedByEffectiveUser(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// closeFiles attempts every release even after one close error so an error
// report does not leave the advisory lock intentionally held.
func (journal *Journal) closeFiles() error {
	failed := false
	if journal.log != nil {
		if err := journal.ops.closeLog(journal.log); err != nil {
			failed = true
		}
	}
	if journal.lock != nil {
		if err := syscall.Flock(int(journal.lock.Fd()), syscall.LOCK_UN); err != nil {
			failed = true
		}
		if err := journal.lock.Close(); err != nil {
			failed = true
		}
	}
	if journal.root != nil {
		if err := journal.root.Close(); err != nil {
			failed = true
		}
	}
	if failed {
		return ErrStorage
	}
	return nil
}

// Close releases the advisory lock after closing the log. It is idempotent.
func (journal *Journal) Close() error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil
	}
	journal.closed = true
	return journal.closeFiles()
}
