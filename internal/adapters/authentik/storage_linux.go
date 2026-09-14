//go:build linux

package authentik

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func (adapter *Adapter) readDirectoryIdentities(spec managedSpec) (fileIdentity, fileIdentity, fileIdentity, error) {
	componentInfo, err := adapter.dataRoot.Lstat(spec.ComponentID)
	if err != nil || !componentInfo.IsDir() || componentInfo.Mode().Perm() != 0o700 || !ownedByEffectiveUser(componentInfo) {
		return fileIdentity{}, fileIdentity{}, fileIdentity{}, ErrUnsafeState
	}
	component, err := adapter.dataRoot.OpenRoot(spec.ComponentID)
	if err != nil {
		return fileIdentity{}, fileIdentity{}, fileIdentity{}, ErrUnsafeState
	}
	defer component.Close()
	read := func(name string) (fileIdentity, error) {
		info, err := component.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return fileIdentity{}, ErrUnsafeState
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != spec.Specification.UID || stat.Gid != spec.Specification.GID {
			return fileIdentity{}, ErrUnsafeState
		}
		return identityFrom(info), nil
	}
	data, e := read("data")
	if e != nil {
		return fileIdentity{}, fileIdentity{}, fileIdentity{}, e
	}
	certs, e := read("certs")
	if e != nil {
		return fileIdentity{}, fileIdentity{}, fileIdentity{}, e
	}
	templates, e := read("templates")
	if e != nil {
		return fileIdentity{}, fileIdentity{}, fileIdentity{}, e
	}
	return data, certs, templates, nil
}

func (adapter *Adapter) readSecretIdentity(spec managedSpec, name, revision string) (fileIdentity, error) {
	file, err := adapter.secretRoot.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fileIdentity{}, ErrSecret
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o007 != 0 || info.Mode().Perm()&0o222 != 0 || linkCount(info) != 1 || info.Size() < 1 || info.Size() > MaxSecretBytes {
		return fileIdentity{}, ErrSecret
	}
	if name == spec.ComponentID+"-secret-key" && info.Size() < 50 {
		return fileIdentity{}, ErrSecret
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) && stat.Uid != spec.Specification.UID || !((stat.Uid == spec.Specification.UID || stat.Uid == uint32(os.Geteuid())) && info.Mode().Perm()&0o400 != 0 || stat.Gid == spec.Specification.GID && info.Mode().Perm()&0o040 != 0 || stat.Uid == 0 && info.Mode().Perm()&0o004 != 0) {
		return fileIdentity{}, ErrSecret
	}
	value, err := io.ReadAll(io.LimitReader(file, MaxSecretBytes+1))
	if err != nil || len(value) > MaxSecretBytes || bytes.IndexAny(value, "\x00\r\n") >= 0 || digest(value) != revision {
		return fileIdentity{}, ErrSecret
	}
	return identityFrom(info), nil
}

func identityFrom(info os.FileInfo) fileIdentity {
	stat := info.Sys().(*syscall.Stat_t)
	return fileIdentity{device: uint64(stat.Dev), inode: stat.Ino, uid: stat.Uid, gid: stat.Gid, mode: uint32(info.Mode().Perm()), size: info.Size()}
}
func identityJSON(value fileIdentity) fileIdentityJSON {
	return fileIdentityJSON{Device: value.device, Inode: value.inode, UID: value.uid, GID: value.gid, Mode: value.mode, Size: value.size}
}
func identityMatches(left fileIdentityJSON, right fileIdentity) bool {
	return left.Device == right.device && left.Inode == right.inode && left.UID == right.uid && left.GID == right.gid && left.Mode == right.mode && left.Size == right.size
}
func directoryIdentityMatches(left fileIdentityJSON, right fileIdentity) bool {
	return left.Device == right.device && left.Inode == right.inode && left.UID == right.uid && left.GID == right.gid && left.Mode == right.mode
}
func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}
func ownedByEffectiveUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func (adapter *Adapter) dataDir(component, name string) string {
	return filepath.Join(adapter.dataPath, component, name)
}
func (adapter *Adapter) secretFile(component, suffix string) string {
	return filepath.Join(adapter.secretPath, component+suffix)
}
