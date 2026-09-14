package postgresql

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hexIDPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	imagePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*@sha256:[0-9a-f]{64}$`)
)

func validID(value string) bool {
	return len(value) >= 1 && len(value) <= 63 && idPattern.MatchString(value)
}

func validPrefix(value string) bool {
	return len(value) >= 1 && len(value) <= 24 && idPattern.MatchString(value)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func imageDigest(image string) string {
	index := strings.LastIndex(image, "@")
	if index < 0 {
		return ""
	}
	return image[index+1:]
}

func validateSpecification(spec Specification) error {
	if !imagePattern.MatchString(spec.Image) || !validID(spec.Database) || !validID(spec.Role) || spec.Port == 0 || spec.UID == 0 || spec.GID == 0 {
		return ErrInvalidInput
	}
	return nil
}

func configurationDigest(spec Specification) (string, error) {
	if err := validateSpecification(spec); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", ErrInvalidInput
	}
	return digest(encoded), nil
}

func identityJSON(identity fileIdentity) fileIdentityJSON {
	return fileIdentityJSON{Device: identity.device, Inode: identity.inode, UID: identity.uid, GID: identity.gid, Mode: identity.mode, Size: identity.size}
}

func identityMatches(left fileIdentityJSON, right fileIdentity) bool {
	return left.Device == right.device && left.Inode == right.inode && left.UID == right.uid && left.GID == right.gid && left.Mode == right.mode && left.Size == right.size
}

func sameDurableIdentity(left, right managedSpec) bool {
	return left.ComponentID == right.ComponentID && left.Specification.Database == right.Specification.Database && left.Specification.Role == right.Specification.Role && left.Specification.Port == right.Specification.Port && left.Specification.UID == right.Specification.UID && left.Specification.GID == right.Specification.GID && left.DataName == right.DataName && left.SecretName == right.SecretName && left.SecretRevision == right.SecretRevision
}
