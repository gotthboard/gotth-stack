package authentik

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hexIDPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	imagePattern   = regexp.MustCompile(`^[a-z0-9]+(?:[._/-][a-z0-9]+)*@sha256:[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^refs/tags/version/2026\.5\.[0-9]+$`)
)

func validID(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, current := range []byte(value) {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' || current == '-' {
			continue
		}
		return false
	}
	return value[len(value)-1] != '-'
}

func validRole(role Role) bool { return role == RoleServer || role == RoleWorker }

func validateSpecification(spec Specification) error {
	if !imagePattern.MatchString(spec.Image) || spec.DatabaseHost != "127.0.0.1" || !validID(spec.Database) || !validID(spec.DatabaseRole) || spec.UID == 0 || spec.GID == 0 {
		return ErrInvalidInput
	}
	ports := []uint16{spec.DatabasePort, spec.HTTPPort, spec.HTTPSPort, spec.WorkerHTTPPort, spec.WorkerHTTPSPort, spec.ServerMetricsPort, spec.WorkerMetricsPort}
	seen := make(map[uint16]bool, len(ports))
	for _, port := range ports {
		if port < 1024 || seen[port] {
			return ErrInvalidInput
		}
		seen[port] = true
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

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func imageDigest(image string) string {
	_, value, ok := strings.Cut(image, "@")
	if !ok {
		return ""
	}
	return value
}

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/"
}

func pathsOverlap(left, right string) bool {
	left += string(filepath.Separator)
	right += string(filepath.Separator)
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

func sameDurableIdentity(left, right managedSpec) bool {
	a, b := left.Specification, right.Specification
	return a.DatabaseHost == b.DatabaseHost && a.DatabasePort == b.DatabasePort && a.Database == b.Database && a.DatabaseRole == b.DatabaseRole && a.UID == b.UID && a.GID == b.GID && left.ComponentID == right.ComponentID && left.DatabaseSecretRevision == right.DatabaseSecretRevision && left.KeySecretRevision == right.KeySecretRevision
}
