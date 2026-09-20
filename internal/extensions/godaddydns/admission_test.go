package godaddydns

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"testing"

	extensioncore "github.com/gotthboard/gotth-extensions/pkg/extensions"
)

func testRequest() Request {
	return Request{Distribution: DistributionCandidate, ForgejoCommit: ExpectedSourceCommit, InstanceID: "11111111-1111-1111-1111-111111111111", Capabilities: []string{"dns.records.create", "dns.records.delete", "dns.records.observe", "dns.records.replace"}, Zones: []string{"example.com"}, RecordTypes: []string{"A", "AAAA", "CAA", "CNAME", "MX", "TXT"}, Environment: "ote", TimeoutSeconds: 15}
}

func expectedManifest() extensioncore.Manifest {
	return extensioncore.Manifest{Schema: extensioncore.ManifestSchema, ID: ExpectedExtensionID, Name: "GOTTH GoDaddy DNS Records", Version: ExpectedVersion, Protocols: []extensioncore.VersionRange{{Name: extensioncore.ControlName, Major: 1, MinMinor: 0, MaxMinor: 0}}, Interfaces: []extensioncore.VersionRange{{Name: ExpectedInterface, Major: 1, MinMinor: 0, MaxMinor: 0}}, Capabilities: append([]string(nil), expectedCapabilities...), Secrets: []extensioncore.SecretRequirement{{ID: ExpectedSecretSlot, Required: true}}}
}

func testArtifact(t testing.TB, mutation string) ([]byte, artifactPins) {
	t.Helper()
	manifest, err := extensioncore.CanonicalManifest(expectedManifest())
	if err != nil {
		t.Fatal(err)
	}
	if digest(manifest) != ExpectedManifestSHA256 {
		t.Fatalf("manifest pin drift: %s", digest(manifest))
	}
	root := "gotth-extension-godaddy-dns-" + ExpectedVersion + "-linux-amd64"
	files := map[string][]byte{"gotth-extension-godaddy-dns": []byte("fake-static-executable"), "manifest.json": manifest, "configuration-metadata.json": []byte(`{"schema":"test"}`), "LICENSE": []byte("test license\n")}
	pins := artifactPins{Manifest: digest(files["manifest.json"]), Executable: digest(files["gotth-extension-godaddy-dns"]), ConfigMetadata: digest(files["configuration-metadata.json"]), License: digest(files["LICENSE"])}
	sums := pins.License + "  LICENSE\n" + pins.ConfigMetadata + "  configuration-metadata.json\n" + pins.Executable + "  gotth-extension-godaddy-dns\n" + pins.Manifest + "  manifest.json\n"
	if mutation == "checksum" {
		sums = "bad\n"
	}
	files["SHA256SUMS"] = []byte(sums)
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	write := func(name string, mode int64, typ byte, body []byte, link string) {
		t.Helper()
		header := &tar.Header{Name: name, Mode: mode, Typeflag: typ, Size: int64(len(body)), Linkname: link, Uid: 0, Gid: 0, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(body) > 0 {
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(root+"/", 0o755, tar.TypeDir, nil, "")
	order := []string{"LICENSE", "SHA256SUMS", "configuration-metadata.json", "gotth-extension-godaddy-dns", "manifest.json"}
	for _, name := range order {
		headerName, mode, typ, link := root+"/"+name, int64(0o644), byte(tar.TypeReg), ""
		if name == "gotth-extension-godaddy-dns" {
			mode = 0o755
		}
		if mutation == "traversal" && name == "LICENSE" {
			headerName = root + "/../evil"
		}
		if mutation == "link" && name == "LICENSE" {
			typ, link, files[name] = tar.TypeSymlink, "manifest.json", nil
		}
		if mutation == "mode" && name == "gotth-extension-godaddy-dns" {
			mode = 0o777
		}
		write(headerName, mode, typ, files[name], link)
		if mutation == "duplicate" && name == "manifest.json" {
			write(headerName, mode, typ, files[name], link)
		}
	}
	if mutation == "extra" {
		write(root+"/extra", 0o644, tar.TypeReg, []byte("extra"), "")
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if mutation == "trailing" {
		compressed.WriteString("trailing")
	}
	archive := compressed.Bytes()
	pins.Archive = digest(archive)
	return archive, pins
}

func FuzzVerifyArtifact(f *testing.F) {
	archive, _ := testArtifact(f, "")
	f.Add(archive)
	f.Add([]byte{})
	f.Add([]byte("not a gzip stream"))
	f.Fuzz(func(t *testing.T, value []byte) {
		if len(value) > 1<<20 {
			t.Skip()
		}
		pins := expectedPins
		pins.Archive = digest(value)
		_, _ = verifyArtifact(bytes.NewReader(value), pins)
	})
}

func TestVerifyAndAdmitCandidateAndPublished(t *testing.T) {
	t.Parallel()
	archive, pins := testArtifact(t, "")
	request := testRequest()
	candidate, err := verifyAndAdmit(bytes.NewReader(archive), request, pins)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ReleaseReady || candidate.ManifestSHA256 != ExpectedManifestSHA256 || candidate.AdmissionSHA256 == "" || digest(candidate.Executable) != pins.Executable {
		t.Fatalf("candidate result: %+v", candidate)
	}
	var binding runtimeBinding
	if err := json.Unmarshal(candidate.RuntimeBinding, &binding); err != nil {
		t.Fatal(err)
	}
	if binding.SessionSHA256 != candidate.SessionSHA256 || !reflectStrings(binding.Capabilities, request.Capabilities) {
		t.Fatalf("binding mismatch: %+v", binding)
	}
	request.Distribution, request.GitHubCommit = DistributionPublished, ExpectedSourceCommit
	pins.PublicationAvailable = true
	published, err := verifyAndAdmit(bytes.NewReader(archive), request, pins)
	if err != nil {
		t.Fatal(err)
	}
	if !published.ReleaseReady || published.AdmissionSHA256 == candidate.AdmissionSHA256 {
		t.Fatal("distribution state not bound")
	}
	again, err := verifyAndAdmit(bytes.NewReader(archive), request, pins)
	if err != nil || !bytes.Equal(again.RuntimeBinding, published.RuntimeBinding) || again.AdmissionSHA256 != published.AdmissionSHA256 {
		t.Fatal("admission is not deterministic")
	}
}

func TestProductionPinsRejectSyntheticArtifact(t *testing.T) {
	t.Parallel()
	archive, _ := testArtifact(t, "")
	if _, err := VerifyAndAdmit(bytes.NewReader(archive), testRequest()); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("synthetic artifact: %v", err)
	}
}

func TestProductionProfileRejectsUnpublishedDistribution(t *testing.T) {
	t.Parallel()
	request := testRequest()
	request.Distribution = DistributionPublished
	request.GitHubCommit = ExpectedSourceCommit
	if validRequest(request, expectedPins.PublicationAvailable) {
		t.Fatal("unpublished production profile accepted published distribution")
	}
}

func TestArtifactHostileMatrix(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"checksum", "traversal", "link", "mode", "duplicate", "extra", "trailing"} {
		t.Run(mutation, func(t *testing.T) {
			archive, pins := testArtifact(t, mutation)
			if _, err := verifyArtifact(bytes.NewReader(archive), pins); !errors.Is(err, ErrInvalidArtifact) {
				t.Fatalf("%s accepted: %v", mutation, err)
			}
		})
	}
	archive, pins := testArtifact(t, "")
	pins.Archive = strings64("0")
	if _, err := verifyArtifact(bytes.NewReader(archive), pins); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("wrong digest: %v", err)
	}
	if _, err := verifyArtifact(nil, pins); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("nil artifact: %v", err)
	}
}

func TestRequestRejectionMatrix(t *testing.T) {
	t.Parallel()
	base := testRequest()
	tests := []func(*Request){func(v *Request) { v.Distribution = "other" }, func(v *Request) { v.ForgejoCommit = "" }, func(v *Request) { v.GitHubCommit = ExpectedSourceCommit }, func(v *Request) { v.InstanceID = "bad" }, func(v *Request) { v.Capabilities = nil }, func(v *Request) { v.Capabilities = []string{"dns.records.observe", "dns.records.create"} }, func(v *Request) { v.Capabilities = []string{"dns.zone.delete"} }, func(v *Request) { v.Zones = []string{"EXAMPLE.com"} }, func(v *Request) { v.Zones = []string{"example.com", "example.com"} }, func(v *Request) { v.RecordTypes = []string{"NS"} }, func(v *Request) { v.Environment = "custom" }, func(v *Request) { v.TimeoutSeconds = 0 }}
	for i, mutate := range tests {
		value := base
		value.Capabilities = append([]string(nil), base.Capabilities...)
		value.Zones = append([]string(nil), base.Zones...)
		value.RecordTypes = append([]string(nil), base.RecordTypes...)
		mutate(&value)
		if validRequest(value, false) {
			t.Fatalf("invalid request %d accepted", i)
		}
	}
	published := base
	published.Distribution = DistributionPublished
	published.GitHubCommit = ExpectedSourceCommit
	if !validRequest(published, true) {
		t.Fatal("published parity rejected")
	}
}

func reflectStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func strings64(v string) string {
	return v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v + v
}
