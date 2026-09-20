package mailruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
)

var (
	idPattern          = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	hexIDPattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	containerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func validID(value string) bool { return idPattern.MatchString(value) }

func validCandidate(candidate Candidate, definition roleDefinition) bool {
	return validID(candidate.OperationID) && validID(candidate.ComponentID) && versionPattern.MatchString(candidate.ProductVersion) && commitPattern.MatchString(candidate.SourceCommit) && digestPattern.MatchString(candidate.ConfigurationDigest) && candidate.ConfigurationSize > 0 && candidate.ConfigurationSize <= MaxConfigurationArchiveBytes && validMembers(candidate.ConfigurationMembers) && digestPattern.MatchString(candidate.SecretRevisionDigest) && validImage(candidate.Image, imageDigest(candidate.Image), definition.repository)
}

func imageDigest(image string) string {
	_, value, found := strings.Cut(image, "@")
	if !found {
		return ""
	}
	return value
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateBinary(path, expected string) error {
	if path == "" || !digestPattern.MatchString(expected) {
		return ErrExecutable
	}
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
	if _, err := io.Copy(hasher, io.LimitReader(file, MaxBinaryBytes+1)); err != nil || "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != expected {
		return ErrExecutable
	}
	return nil
}

func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}
