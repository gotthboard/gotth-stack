// Package mailruntime implements the bounded runtime mechanisms for GOTTH
// Mail roles. It grants no controller or deployment authority by itself.
package mailruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

const (
	ManifestSchemaVersion = 1
	MaxManifestBytes      = 1 << 20
	maxJSONDepth          = 32
)

const (
	ForgejoRepository = "https://git.dannyhunn.com/gotthboard/gotth-mail"
	GitHubRepository  = "https://github.com/gotthboard/gotth-mail"
)

var (
	ErrInvalidJSON     = errors.New("mail release manifest JSON is invalid")
	ErrDuplicateName   = errors.New("mail release manifest contains a duplicate object name")
	ErrTooDeep         = errors.New("mail release manifest JSON nesting is too deep")
	ErrManifestLarge   = errors.New("mail release manifest is too large")
	ErrInvalidManifest = errors.New("mail release manifest contract is invalid")
	ErrInvalidArchive  = errors.New("mail configuration archive contract is invalid")
)

type Role string

const (
	RoleControlPlane Role = "control-plane"
	RoleDovecot      Role = "dovecot"
	RoleFront        Role = "front"
	RolePostfix      Role = "postfix"
	RoleRspamd       Role = "rspamd"
)

var requiredRoles = []Role{RoleControlPlane, RoleDovecot, RoleFront, RolePostfix, RoleRspamd}

type FileArtifact struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type RoleArtifact struct {
	Role        Role   `json:"role"`
	Image       string `json:"image"`
	ImageDigest string `json:"image_digest"`
}

type ExtensionArtifact struct {
	Repository     string `json:"repository"`
	Version        string `json:"version"`
	ArtifactDigest string `json:"artifact_digest"`
	ManifestDigest string `json:"manifest_digest"`
}

type SchemaRange struct {
	Minimum uint32 `json:"minimum"`
	Current uint32 `json:"current"`
	Maximum uint32 `json:"maximum"`
}

type BuildIdentity struct {
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

type ReleaseManifest struct {
	SchemaVersion        int                 `json:"schema_version"`
	ProductVersion       string              `json:"product_version"`
	Tag                  string              `json:"tag"`
	TagObject            string              `json:"tag_object"`
	SourceCommit         string              `json:"source_commit"`
	ForgejoRefCommit     string              `json:"forgejo_ref_commit"`
	GitHubRefCommit      string              `json:"github_ref_commit"`
	ForgejoRepository    string              `json:"forgejo_repository"`
	GitHubRepository     string              `json:"github_repository"`
	ConfigurationArchive FileArtifact        `json:"configuration_archive"`
	ConfigurationMembers []FileArtifact      `json:"configuration_members"`
	Roles                []RoleArtifact      `json:"roles"`
	Extensions           []ExtensionArtifact `json:"extensions"`
	Schema               SchemaRange         `json:"schema"`
	Build                BuildIdentity       `json:"build"`
}

type ParsedManifest struct {
	Manifest ReleaseManifest
	Digest   string
}

var (
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern    = regexp.MustCompile(`^1\.0\.0-(?:alpha|beta)\.[1-9][0-9]*$`)
	fileNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	memberNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(?:/[a-z0-9][a-z0-9._-]*)*$`)
	repositoryPattern = regexp.MustCompile(`^github\.com/gotthboard/gotth-extension-[a-z0-9]+(?:-[a-z0-9]+)*$`)
	imagePattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
)

var roleImageRepositories = map[Role]string{
	RoleControlPlane: "ghcr.io/gotthboard/gotth-mail-control-plane",
	RoleDovecot:      "ghcr.io/gotthboard/gotth-mail-dovecot",
	RoleFront:        "ghcr.io/gotthboard/gotth-mail-front",
	RolePostfix:      "ghcr.io/gotthboard/gotth-mail-postfix",
	RoleRspamd:       "ghcr.io/gotthboard/gotth-mail-rspamd",
}

func ParseReleaseManifest(reader io.Reader) (ParsedManifest, error) {
	if nilReader(reader) {
		return ParsedManifest{}, ErrInvalidJSON
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxManifestBytes+1))
	if err != nil {
		return ParsedManifest{}, ErrInvalidJSON
	}
	if len(raw) > MaxManifestBytes {
		return ParsedManifest{}, ErrManifestLarge
	}
	if err := inspectJSON(raw); err != nil {
		return ParsedManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest ReleaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ParsedManifest{}, ErrInvalidJSON
	}
	if err := requireJSONEnd(decoder); err != nil {
		return ParsedManifest{}, err
	}
	if !validReleaseManifest(manifest) {
		return ParsedManifest{}, ErrInvalidManifest
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return ParsedManifest{}, ErrInvalidManifest
	}
	sum := sha256.Sum256(raw)
	return ParsedManifest{Manifest: manifest, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func nilReader(reader io.Reader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func validReleaseManifest(manifest ReleaseManifest) bool {
	if manifest.SchemaVersion != ManifestSchemaVersion || !versionPattern.MatchString(manifest.ProductVersion) || manifest.Tag != "v"+manifest.ProductVersion || !commitPattern.MatchString(manifest.TagObject) || !commitPattern.MatchString(manifest.SourceCommit) || manifest.ForgejoRefCommit != manifest.SourceCommit || manifest.GitHubRefCommit != manifest.SourceCommit || manifest.ForgejoRepository != ForgejoRepository || manifest.GitHubRepository != GitHubRepository {
		return false
	}
	if !validFileArtifact(manifest.ConfigurationArchive) || !strings.HasSuffix(manifest.ConfigurationArchive.Name, ".tar") || manifest.ConfigurationArchive.Size > MaxConfigurationArchiveBytes || !validMembers(manifest.ConfigurationMembers) || manifest.Schema.Minimum == 0 || manifest.Schema.Minimum > manifest.Schema.Current || manifest.Schema.Current > manifest.Schema.Maximum || manifest.Build.GoVersion != "go1.26.6" || manifest.Build.GOOS != "linux" || manifest.Build.GOARCH != "amd64" {
		return false
	}
	if len(manifest.Roles) != len(requiredRoles) {
		return false
	}
	for index, role := range manifest.Roles {
		if role.Role != requiredRoles[index] || !validImage(role.Image, role.ImageDigest, roleImageRepositories[role.Role]) {
			return false
		}
	}
	if len(manifest.Extensions) > 32 {
		return false
	}
	previous := ""
	for _, extension := range manifest.Extensions {
		if !repositoryPattern.MatchString(extension.Repository) || extension.Repository <= previous || !versionPattern.MatchString(extension.Version) || !digestPattern.MatchString(extension.ArtifactDigest) || !digestPattern.MatchString(extension.ManifestDigest) {
			return false
		}
		previous = extension.Repository
	}
	return true
}

func validMembers(members []FileArtifact) bool {
	if len(members) == 0 || len(members) > 4096 {
		return false
	}
	previous := ""
	var total int64
	for _, member := range members {
		if len(member.Name) > 255 || !memberNamePattern.MatchString(member.Name) || strings.Contains(member.Name, "..") || member.Name <= previous || !digestPattern.MatchString(member.Digest) || member.Size < 0 || member.Size > MaxConfigurationMemberBytes || total > MaxConfigurationExpandedBytes-member.Size {
			return false
		}
		previous = member.Name
		total += member.Size
	}
	return total > 0
}

func validFileArtifact(artifact FileArtifact) bool {
	return fileNamePattern.MatchString(artifact.Name) && digestPattern.MatchString(artifact.Digest) && artifact.Size > 0 && artifact.Size <= 1<<30
}

func validImage(image, expectedDigest, expectedRepository string) bool {
	if len(image) < 1 || len(image) > 512 || strings.TrimSpace(image) != image || strings.ContainsAny(image, "\x00\r\n ") || !digestPattern.MatchString(expectedDigest) {
		return false
	}
	repository, digestValue, found := strings.Cut(image, "@")
	return found && repository == expectedRepository && imagePattern.MatchString(repository) && !strings.Contains(repository, "@") && digestValue == expectedDigest
}

func inspectJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := inspectJSONValue(decoder, 0); err != nil {
		return err
	}
	return requireJSONEnd(decoder)
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidJSON
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return ErrInvalidJSON
	}
	if depth >= maxJSONDepth {
		return ErrTooDeep
	}
	if delimiter == '{' {
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidJSON
			}
			name, ok := nameToken.(string)
			if !ok {
				return ErrInvalidJSON
			}
			if _, exists := seen[name]; exists {
				return ErrDuplicateName
			}
			seen[name] = struct{}{}
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	} else {
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return ErrInvalidJSON
	}
	wanted := json.Delim('}')
	if delimiter == '[' {
		wanted = ']'
	}
	if closing != wanted {
		return ErrInvalidJSON
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); errors.Is(err, io.EOF) {
		return nil
	}
	return ErrInvalidJSON
}

func RequiredRoles() []Role {
	return slices.Clone(requiredRoles)
}
