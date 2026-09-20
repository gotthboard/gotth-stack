package mailruntime

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

type archiveInput struct {
	name     string
	contents string
	mutate   func(*tar.Header)
}

func configurationArchive(t *testing.T, inputs []archiveInput, trailer []byte) ([]byte, []FileArtifact) {
	t.Helper()
	var value bytes.Buffer
	writer := tar.NewWriter(&value)
	members := make([]FileArtifact, 0, len(inputs))
	for _, input := range inputs {
		header := &tar.Header{Name: input.name, Mode: configurationMode, Size: int64(len(input.contents)), ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if input.mutate != nil {
			input.mutate(header)
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(input.contents)); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(input.contents))
		members = append(members, FileArtifact{Name: input.name, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(input.contents))})
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return append(value.Bytes(), trailer...), members
}

func archiveManifest(t *testing.T, value []byte, members []FileArtifact) ReleaseManifest {
	t.Helper()
	manifest := validManifest()
	sum := sha256.Sum256(value)
	manifest.ConfigurationArchive = FileArtifact{Name: "gotth-mail-config.tar", Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(value))}
	manifest.ConfigurationMembers = members
	return manifest
}

func TestVerifyConfigurationArchive(t *testing.T) {
	value, members := configurationArchive(t, []archiveInput{{name: "control/config.json", contents: "{}\n"}, {name: "postfix/main.cf", contents: "compatibility_level = 3.6\n"}}, nil)
	if err := VerifyConfigurationArchive(bytes.NewReader(value), archiveManifest(t, value, members)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyConfigurationArchiveRejectsBoundaryViolations(t *testing.T) {
	base := []archiveInput{{name: "control/config.json", contents: "{}\n"}, {name: "postfix/main.cf", contents: "config\n"}}
	cases := []struct {
		name    string
		inputs  []archiveInput
		trailer []byte
		mutate  func(*ReleaseManifest)
	}{
		{name: "nil reader"},
		{name: "wrong digest", inputs: base, mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Digest = testDigest }},
		{name: "wrong size", inputs: base, mutate: func(v *ReleaseManifest) { v.ConfigurationArchive.Size-- }},
		{name: "member digest", inputs: base, mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Digest = testDigest }},
		{name: "member size", inputs: base, mutate: func(v *ReleaseManifest) { v.ConfigurationMembers[0].Size++ }},
		{name: "member order", inputs: []archiveInput{base[1], base[0]}, mutate: func(v *ReleaseManifest) {
			v.ConfigurationMembers[0], v.ConfigurationMembers[1] = v.ConfigurationMembers[1], v.ConfigurationMembers[0]
		}},
		{name: "missing member", inputs: base, mutate: func(v *ReleaseManifest) { v.ConfigurationMembers = v.ConfigurationMembers[:1] }},
		{name: "extra member", inputs: base[:1], mutate: func(v *ReleaseManifest) {
			v.ConfigurationMembers = append(v.ConfigurationMembers, FileArtifact{Name: "z", Digest: testDigest, Size: 1})
		}},
		{name: "nonzero trailer", inputs: base, trailer: []byte{1}},
		{name: "symlink", inputs: []archiveInput{{name: "control/config.json", contents: "", mutate: func(h *tar.Header) { h.Typeflag = tar.TypeSymlink; h.Linkname = "/etc/passwd" }}, {name: "z/keep", contents: "x"}}},
		{name: "hardlink", inputs: []archiveInput{{name: "control/config.json", contents: "", mutate: func(h *tar.Header) { h.Typeflag = tar.TypeLink; h.Linkname = "postfix/main.cf" }}, {name: "z/keep", contents: "x"}}},
		{name: "directory", inputs: []archiveInput{{name: "control", contents: "", mutate: func(h *tar.Header) { h.Typeflag = tar.TypeDir }}, {name: "z/keep", contents: "x"}}},
		{name: "mode", inputs: []archiveInput{{name: "control/config.json", contents: "{}", mutate: func(h *tar.Header) { h.Mode = 0o640 }}}},
		{name: "owner", inputs: []archiveInput{{name: "control/config.json", contents: "{}", mutate: func(h *tar.Header) { h.Uid = 1000 }}}},
		{name: "mtime", inputs: []archiveInput{{name: "control/config.json", contents: "{}", mutate: func(h *tar.Header) { h.ModTime = time.Unix(1, 0) }}}},
		{name: "pax", inputs: []archiveInput{{name: "control/config.json", contents: "{}", mutate: func(h *tar.Header) { h.Format = tar.FormatPAX; h.PAXRecords = map[string]string{"comment": "bad"} }}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputs := tc.inputs
			if inputs == nil {
				inputs = base
			}
			value, members := configurationArchive(t, inputs, tc.trailer)
			manifest := archiveManifest(t, value, members)
			if tc.mutate != nil {
				tc.mutate(&manifest)
			}
			var reader *bytes.Reader
			if tc.name != "nil reader" {
				reader = bytes.NewReader(value)
			}
			if err := VerifyConfigurationArchive(reader, manifest); err != ErrInvalidArchive {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestReleaseManifestRejectsConfigurationExpansionLimits(t *testing.T) {
	manifest := validManifest()
	manifest.ConfigurationMembers[0].Size = MaxConfigurationMemberBytes + 1
	if validReleaseManifest(manifest) {
		t.Fatal("accepted oversized member")
	}
	manifest = validManifest()
	manifest.ConfigurationMembers[0].Name = strings.Repeat("a", 256)
	if validReleaseManifest(manifest) {
		t.Fatal("accepted oversized member name")
	}
}
