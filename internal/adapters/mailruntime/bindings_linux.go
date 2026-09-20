//go:build linux

package mailruntime

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const maxRevisionBytes int64 = 16 << 20

func (adapter *adapter) captureMountIdentities() ([]mountIdentity, error) {
	if len(adapter.mountFiles) != len(adapter.definition.mounts) {
		return nil, ErrUnsafeState
	}
	result := make([]mountIdentity, 0, len(adapter.mountFiles))
	for index, file := range adapter.mountFiles {
		info, err := file.Stat()
		if err != nil {
			return nil, ErrUnsafeState
		}
		result = append(result, mountIdentity{Destination: adapter.definition.mounts[index].destination, Identity: mountIdentityJSON(info)})
	}
	return result, nil
}

func (adapter *adapter) verifyMountIdentities(expected []mountIdentity) error {
	if len(expected) != len(adapter.definition.mounts) || len(adapter.mountFiles) != len(expected) {
		return ErrConflict
	}
	for index, mount := range adapter.definition.mounts {
		if expected[index].Destination != mount.destination {
			return ErrConflict
		}
		opened, err := adapter.mountFiles[index].Stat()
		current, pathErr := os.Lstat(mount.source)
		if err != nil || pathErr != nil || !os.SameFile(opened, current) || expected[index].Identity != mountIdentityJSON(opened) || expected[index].Identity != mountIdentityJSON(current) {
			return ErrConflict
		}
	}
	return nil
}

func (adapter *adapter) verifyConfiguration(members []FileArtifact, expectedDigest string, expectedSize int64) error {
	if !validMembers(members) || adapter.configPath != filepath.Join(filepath.Dir(adapter.configPath), strings.TrimPrefix(expectedDigest, "sha256:")) {
		return ErrConfiguration
	}
	root, err := os.OpenRoot(adapter.configPath)
	if err != nil {
		return ErrConfiguration
	}
	defer root.Close()
	expected := make(map[string]FileArtifact, len(members))
	directories := map[string]bool{".": true}
	for _, member := range members {
		expected[member.Name] = member
		for directory := filepath.Dir(member.Name); directory != "."; directory = filepath.Dir(directory) {
			directories[filepath.ToSlash(directory)] = true
		}
	}
	seen := make(map[string]bool, len(members))
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || strings.Contains(name, "\\") {
			return ErrConfiguration
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrConfiguration
		}
		if entry.IsDir() {
			if !directories[name] {
				return ErrConfiguration
			}
			info, err := entry.Info()
			if err != nil || info.Mode().Perm()&0o022 != 0 {
				return ErrConfiguration
			}
			return nil
		}
		wanted, ok := expected[name]
		if !ok || seen[name] {
			return ErrConfiguration
		}
		file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return ErrConfiguration
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != configurationMode || linkCount(info) != 1 || info.Size() != wanted.Size {
			_ = file.Close()
			return ErrConfiguration
		}
		hasher := sha256.New()
		written, copyErr := io.Copy(hasher, io.LimitReader(file, MaxConfigurationMemberBytes+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != wanted.Size || "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != wanted.Digest {
			return ErrConfiguration
		}
		seen[name] = true
		return nil
	})
	if err != nil || len(seen) != len(expected) {
		return ErrConfiguration
	}
	hasher := sha256.New()
	counter := &countingWriter{writer: hasher}
	archive := tar.NewWriter(counter)
	for _, member := range members {
		header := &tar.Header{Name: member.Name, Mode: configurationMode, Size: member.Size, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if err := archive.WriteHeader(header); err != nil {
			return ErrConfiguration
		}
		file, err := root.OpenFile(member.Name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return ErrConfiguration
		}
		written, copyErr := io.Copy(archive, io.LimitReader(file, MaxConfigurationMemberBytes+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != member.Size {
			return ErrConfiguration
		}
	}
	if err := archive.Close(); err != nil || counter.count != expectedSize || "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != expectedDigest {
		return ErrConfiguration
	}
	return nil
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	written, err := writer.writer.Write(value)
	writer.count += int64(written)
	return written, err
}

type revisionMember struct {
	Destination string `json:"destination"`
	Name        string `json:"name"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
}

func (adapter *adapter) revisionDigest() (string, error) {
	members := make([]revisionMember, 0)
	for _, mount := range adapter.definition.mounts {
		if !mount.revision {
			continue
		}
		if mount.kind == mountRegularFile {
			member, err := hashRevisionFile(mount, "")
			if err != nil {
				return "", err
			}
			members = append(members, member)
			continue
		}
		root, err := os.OpenRoot(mount.source)
		if err != nil {
			return "", ErrSecret
		}
		err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.Type()&os.ModeSymlink != 0 {
				return ErrSecret
			}
			if entry.IsDir() {
				return nil
			}
			member, err := hashRevisionRootFile(root, mount.destination, name)
			if err != nil {
				return err
			}
			members = append(members, member)
			return nil
		})
		_ = root.Close()
		if err != nil {
			return "", err
		}
	}
	sort.Slice(members, func(left, right int) bool {
		if members[left].Destination != members[right].Destination {
			return members[left].Destination < members[right].Destination
		}
		return members[left].Name < members[right].Name
	})
	encoded, err := json.Marshal(members)
	if err != nil {
		return "", ErrStorage
	}
	return digest(encoded), nil
}

func hashRevisionFile(mount mountSpec, name string) (revisionMember, error) {
	file, err := os.OpenFile(mount.source, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return revisionMember{}, ErrSecret
	}
	defer file.Close()
	return hashOpenedRevision(file, mount.destination, name)
}

func hashRevisionRootFile(root *os.Root, destination, name string) (revisionMember, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return revisionMember{}, ErrSecret
	}
	defer file.Close()
	return hashOpenedRevision(file, destination, name)
}

func hashOpenedRevision(file *os.File, destination, name string) (revisionMember, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o007 != 0 || info.Mode().Perm()&0o022 != 0 || linkCount(info) != 1 || info.Size() < 1 || info.Size() > maxRevisionBytes {
		return revisionMember{}, ErrSecret
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) && stat.Uid != 1000) {
		return revisionMember{}, ErrSecret
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(file, maxRevisionBytes+1))
	if err != nil || written != info.Size() {
		return revisionMember{}, ErrSecret
	}
	return revisionMember{Destination: destination, Name: name, Digest: "sha256:" + hex.EncodeToString(hasher.Sum(nil)), Size: info.Size()}, nil
}

func identityJSON(info os.FileInfo) fileIdentityJSON {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentityJSON{}
	}
	return fileIdentityJSON{Device: uint64(stat.Dev), Inode: stat.Ino, UID: stat.Uid, GID: stat.Gid, Mode: uint32(info.Mode().Perm()), Size: info.Size()}
}

func mountIdentityJSON(info os.FileInfo) fileIdentityJSON {
	identity := identityJSON(info)
	if info.IsDir() {
		identity.Size = 0
	}
	return identity
}

func sameIdentities(left, right []mountIdentity) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeBindingError(err error) error {
	if errors.Is(err, ErrConfiguration) || errors.Is(err, ErrSecret) {
		return err
	}
	return ErrConflict
}
