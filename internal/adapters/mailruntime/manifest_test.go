package mailruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func validManifest() ReleaseManifest {
	roles := make([]RoleArtifact, 0, len(requiredRoles))
	for _, role := range requiredRoles {
		roles = append(roles, RoleArtifact{Role: role, Image: roleImageRepositories[role] + "@" + testDigest, ImageDigest: testDigest})
	}
	return ReleaseManifest{
		SchemaVersion: ManifestSchemaVersion, ProductVersion: "1.0.0-alpha.1", Tag: "v1.0.0-alpha.1",
		TagObject: strings.Repeat("c", 40), SourceCommit: strings.Repeat("b", 40), ForgejoRefCommit: strings.Repeat("b", 40), GitHubRefCommit: strings.Repeat("b", 40), ForgejoRepository: ForgejoRepository, GitHubRepository: GitHubRepository,
		ConfigurationArchive: FileArtifact{Name: "gotth-mail-config.tar", Digest: testDigest, Size: 4096},
		ConfigurationMembers: []FileArtifact{{Name: "control/config.json", Digest: testDigest, Size: 128}, {Name: "postfix/main.cf", Digest: testDigest, Size: 256}},
		Roles:                roles,
		Extensions:           []ExtensionArtifact{{Repository: "github.com/gotthboard/gotth-extension-webhook", Version: "1.0.0-alpha.1", ArtifactDigest: testDigest, ManifestDigest: testDigest}},
		Schema:               SchemaRange{Minimum: 17, Current: 17, Maximum: 17},
		Build:                BuildIdentity{GoVersion: "go1.26.6", GOOS: "linux", GOARCH: "amd64", BuildDateEpoch: 1789938083},
	}
}

func encodeManifest(t *testing.T, manifest ReleaseManifest) []byte {
	t.Helper()
	value, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return append(value, '\n')
}

func TestParseReleaseManifest(t *testing.T) {
	raw := encodeManifest(t, validManifest())
	parsed, err := ParseReleaseManifest(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Manifest.Tag != "v1.0.0-alpha.1" || !digestPattern.MatchString(parsed.Digest) {
		t.Fatalf("parsed=%#v", parsed)
	}
	roles := RequiredRoles()
	roles[0] = "corrupt"
	if RequiredRoles()[0] != RoleControlPlane {
		t.Fatal("RequiredRoles returned shared storage")
	}
}

func TestParseReleaseManifestRejectsJSONBoundary(t *testing.T) {
	valid := encodeManifest(t, validManifest())
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{name: "nil", want: ErrInvalidJSON},
		{name: "malformed", raw: []byte(`{"schema_version":`), want: ErrInvalidJSON},
		{name: "noncanonical", raw: append([]byte{' '}, valid...), want: ErrInvalidManifest},
		{name: "duplicate", raw: []byte(`{"schema_version":1,"schema_version":1}`), want: ErrDuplicateName},
		{name: "unknown", raw: append(valid[:len(valid)-2], []byte(`,"unknown":true}`)...), want: ErrInvalidJSON},
		{name: "trailing", raw: append(valid, []byte(`{}`)...), want: ErrInvalidJSON},
		{name: "large", raw: bytes.Repeat([]byte{' '}, MaxManifestBytes+1), want: ErrManifestLarge},
		{name: "deep", raw: []byte(strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1)), want: ErrTooDeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.raw == nil {
				_, err := ParseReleaseManifest(nil)
				if !errors.Is(err, tc.want) {
					t.Fatalf("error=%v want=%v", err, tc.want)
				}
				return
			}
			_, err := ParseReleaseManifest(bytes.NewReader(tc.raw))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestReleaseManifestValidationMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ReleaseManifest)
	}{
		{name: "schema", mutate: func(v *ReleaseManifest) { v.SchemaVersion = 2 }},
		{name: "version", mutate: func(v *ReleaseManifest) { v.ProductVersion = "1.0.0-alpha.0" }},
		{name: "tag", mutate: func(v *ReleaseManifest) { v.Tag = "v1.0.0-alpha.2" }},
		{name: "tag object", mutate: func(v *ReleaseManifest) { v.TagObject = "main" }},
		{name: "commit", mutate: func(v *ReleaseManifest) { v.SourceCommit = "main" }},
		{name: "forgejo ref", mutate: func(v *ReleaseManifest) { v.ForgejoRefCommit = strings.Repeat("d", 40) }},
		{name: "github ref", mutate: func(v *ReleaseManifest) { v.GitHubRefCommit = strings.Repeat("d", 40) }},
		{name: "forgejo", mutate: func(v *ReleaseManifest) { v.ForgejoRepository = GitHubRepository }},
		{name: "github", mutate: func(v *ReleaseManifest) { v.GitHubRepository += ".git" }},
		{name: "file name", mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Name = "../config" }},
		{name: "file digest", mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Digest = "bad" }},
		{name: "file size zero", mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Size = 0 }},
		{name: "file size large", mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Size = (1 << 30) + 1 }},
		{name: "members empty", mutate: func(v *ReleaseManifest) { v.ConfigurationMembers = nil }},
		{name: "member traversal", mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Name = "../secret" }},
		{name: "member absolute", mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Name = "/etc/passwd" }},
		{name: "member order", mutate: func(v *ReleaseManifest) {
			v.ConfigurationMembers[0], v.ConfigurationMembers[1] = v.ConfigurationMembers[1], v.ConfigurationMembers[0]
		}},
		{name: "member duplicate", mutate: func(v *ReleaseManifest) {
			v.ConfigurationMembers = append(v.ConfigurationMembers, v.ConfigurationMembers[1])
		}},
		{name: "member digest", mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Digest = "bad" }},
		{name: "member negative", mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Size = -1 }},
		{name: "member empty archive", mutate: func(v *ReleaseManifest) {
			for index := range v.ConfigurationMembers {
				v.ConfigurationMembers[index].Size = 0
			}
		}},
		{name: "schema minimum", mutate: func(v *ReleaseManifest) { v.Schema.Minimum = 0 }},
		{name: "schema order", mutate: func(v *ReleaseManifest) { v.Schema.Minimum = 18 }},
		{name: "schema maximum", mutate: func(v *ReleaseManifest) { v.Schema.Maximum = 16 }},
		{name: "go", mutate: func(v *ReleaseManifest) { v.Build.GoVersion = "go1.26.5" }},
		{name: "goos", mutate: func(v *ReleaseManifest) { v.Build.GOOS = "windows" }},
		{name: "goarch", mutate: func(v *ReleaseManifest) { v.Build.GOARCH = "arm64" }},
		{name: "build epoch", mutate: func(v *ReleaseManifest) { v.Build.BuildDateEpoch = 0 }},
		{name: "missing role", mutate: func(v *ReleaseManifest) { v.Roles = v.Roles[:4] }},
		{name: "role order", mutate: func(v *ReleaseManifest) { v.Roles[0], v.Roles[1] = v.Roles[1], v.Roles[0] }},
		{name: "floating image", mutate: func(v *ReleaseManifest) { v.Roles[0].Image = "ghcr.io/gotthboard/gotth-mail-control-plane:latest" }},
		{name: "wrong image repository", mutate: func(v *ReleaseManifest) { v.Roles[0].Image = "ghcr.io/other/gotth-mail-control-plane@" + testDigest }},
		{name: "image digest mismatch", mutate: func(v *ReleaseManifest) {
			v.Roles[0].Image = strings.Replace(v.Roles[0].Image, testDigest, "sha256:"+strings.Repeat("b", 64), 1)
		}},
		{name: "too many extensions", mutate: func(v *ReleaseManifest) { v.Extensions = make([]ExtensionArtifact, 33) }},
		{name: "extension repository", mutate: func(v *ReleaseManifest) { v.Extensions[0].Repository = "github.com/gotthboard/gotth-mail" }},
		{name: "extension version", mutate: func(v *ReleaseManifest) { v.Extensions[0].Version = "dev" }},
		{name: "extension artifact", mutate: func(v *ReleaseManifest) { v.Extensions[0].ArtifactDigest = "bad" }},
		{name: "extension manifest", mutate: func(v *ReleaseManifest) { v.Extensions[0].ManifestDigest = "bad" }},
		{name: "duplicate extension", mutate: func(v *ReleaseManifest) { v.Extensions = append(v.Extensions, v.Extensions[0]) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest()
			tc.mutate(&manifest)
			_, err := ParseReleaseManifest(bytes.NewReader(encodeManifest(t, manifest)))
			if !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestValidImageRejectsHostileValues(t *testing.T) {
	repository := roleImageRepositories[RoleControlPlane]
	for _, value := range []string{"", " image@" + testDigest, repository + " @" + testDigest, repository + "@@" + testDigest, "repo@" + testDigest, repository + "@sha256:bad"} {
		if validImage(value, testDigest, repository) {
			t.Fatalf("accepted %q", value)
		}
	}
}
