package godaddydns

import (
	"encoding/json"
	"io"
	"reflect"
	"sort"
	"strings"

	extensioncore "github.com/gotthboard/gotth-extensions/pkg/extensions"
)

type runtimeBinding struct {
	Schema         string   `json:"schema"`
	InstanceID     string   `json:"instance_id"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	GrantSHA256    string   `json:"grant_sha256"`
	SessionSHA256  string   `json:"session_sha256"`
	Capabilities   []string `json:"capabilities"`
}
type providerConfiguration struct {
	Schema         string   `json:"schema"`
	Environment    string   `json:"environment"`
	Zones          []string `json:"zones"`
	RecordTypes    []string `json:"record_types"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}
type admissionWire struct {
	Schema                      string             `json:"schema"`
	SourceCommit                string             `json:"source_commit"`
	FoundationCommit            string             `json:"foundation_commit"`
	Distribution                DistributionStatus `json:"distribution"`
	ForgejoRepository           string             `json:"forgejo_repository"`
	ForgejoCommit               string             `json:"forgejo_commit"`
	GitHubRepository            string             `json:"github_repository"`
	GitHubCommit                string             `json:"github_commit"`
	Version                     string             `json:"version"`
	ArchiveSHA256               string             `json:"archive_sha256"`
	ManifestSHA256              string             `json:"manifest_sha256"`
	ExecutableSHA256            string             `json:"executable_sha256"`
	ConfigurationMetadataSHA256 string             `json:"configuration_metadata_sha256"`
	GrantSHA256                 string             `json:"grant_sha256"`
	SessionSHA256               string             `json:"session_sha256"`
	RuntimeBindingSHA256        string             `json:"runtime_binding_sha256"`
	ProviderConfigurationSHA256 string             `json:"provider_configuration_sha256"`
}

func VerifyAndAdmit(reader io.Reader, request Request) (Result, error) {
	return verifyAndAdmit(reader, request, expectedPins)
}

func verifyAndAdmit(reader io.Reader, request Request, pins artifactPins) (Result, error) {
	if !validRequest(request) {
		return Result{}, ErrInvalidAdmission
	}
	members, err := verifyArtifact(reader, pins)
	if err != nil {
		return Result{}, err
	}
	manifest, err := extensioncore.ParseManifest(members.Manifest)
	if err != nil || !validManifest(manifest) {
		return Result{}, ErrInvalidArtifact
	}
	manifestDigest, err := extensioncore.ManifestDigest(manifest)
	if err != nil || manifestDigest != ExpectedManifestSHA256 {
		return Result{}, ErrInvalidArtifact
	}
	grant := extensioncore.Grant{Schema: extensioncore.GrantSchema, InstanceID: request.InstanceID, ExtensionID: ExpectedExtensionID, ManifestDigest: manifestDigest, Capabilities: append([]string(nil), request.Capabilities...), Interfaces: []extensioncore.InterfaceGrant{{Name: ExpectedInterface, Major: 1, Minor: 0}}, Secrets: []string{ExpectedSecretSlot}}
	host := extensioncore.HostProfile{Protocols: []extensioncore.VersionRange{{Name: extensioncore.ControlName, Major: 1, MinMinor: 0, MaxMinor: 0}}, Interfaces: []extensioncore.VersionRange{{Name: ExpectedInterface, Major: 1, MinMinor: 0, MaxMinor: 0}}}
	session, err := extensioncore.Negotiate(manifest, grant, host)
	if err != nil {
		return Result{}, ErrInvalidAdmission
	}
	binding := runtimeBinding{Schema: BindingSchema, InstanceID: request.InstanceID, ManifestSHA256: manifestDigest, GrantSHA256: session.GrantDigest, SessionSHA256: session.Fingerprint, Capabilities: append([]string(nil), session.Capabilities...)}
	configuration := providerConfiguration{Schema: ConfigSchema, Environment: request.Environment, Zones: append([]string(nil), request.Zones...), RecordTypes: append([]string(nil), request.RecordTypes...), TimeoutSeconds: request.TimeoutSeconds}
	bindingJSON, err := json.Marshal(binding)
	if err != nil {
		return Result{}, ErrInvalidAdmission
	}
	configurationJSON, err := json.Marshal(configuration)
	if err != nil {
		return Result{}, ErrInvalidAdmission
	}
	wire := admissionWire{Schema: "gotth.stack.extension-admission.v1", SourceCommit: ExpectedSourceCommit, FoundationCommit: ExpectedFoundationCommit, Distribution: request.Distribution, ForgejoRepository: ForgejoRepository, ForgejoCommit: request.ForgejoCommit, GitHubRepository: GitHubRepository, GitHubCommit: request.GitHubCommit, Version: ExpectedVersion, ArchiveSHA256: pins.Archive, ManifestSHA256: manifestDigest, ExecutableSHA256: pins.Executable, ConfigurationMetadataSHA256: pins.ConfigMetadata, GrantSHA256: session.GrantDigest, SessionSHA256: session.Fingerprint, RuntimeBindingSHA256: digest(bindingJSON), ProviderConfigurationSHA256: digest(configurationJSON)}
	wireJSON, err := json.Marshal(wire)
	if err != nil {
		return Result{}, ErrInvalidAdmission
	}
	return Result{Executable: append([]byte(nil), members.Executable...), RuntimeBinding: bindingJSON, ProviderConfiguration: configurationJSON, ManifestSHA256: manifestDigest, GrantSHA256: session.GrantDigest, SessionSHA256: session.Fingerprint, AdmissionSHA256: digest(wireJSON), ReleaseReady: request.Distribution == DistributionPublished}, nil
}

func validRequest(r Request) bool {
	if !validUUID(r.InstanceID) || len(r.Capabilities) < 1 || len(r.Capabilities) > len(expectedCapabilities) || !sortedSubset(r.Capabilities, expectedCapabilities) || len(r.Zones) < 1 || len(r.Zones) > 32 || !sort.StringsAreSorted(r.Zones) || len(r.RecordTypes) < 1 || len(r.RecordTypes) > len(expectedRecordTypes) || !sortedSubset(r.RecordTypes, expectedRecordTypes) || r.Environment != "production" && r.Environment != "ote" || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 30 {
		return false
	}
	for i, zone := range r.Zones {
		if !validZone(zone) || i > 0 && zone == r.Zones[i-1] {
			return false
		}
	}
	switch r.Distribution {
	case DistributionCandidate:
		return r.ForgejoCommit == ExpectedSourceCommit && r.GitHubCommit == ""
	case DistributionPublished:
		return r.ForgejoCommit == ExpectedSourceCommit && r.GitHubCommit == ExpectedSourceCommit
	default:
		return false
	}
}

func validManifest(m extensioncore.Manifest) bool {
	expected := extensioncore.Manifest{Schema: extensioncore.ManifestSchema, ID: ExpectedExtensionID, Name: "GOTTH GoDaddy DNS Records", Version: ExpectedVersion, Protocols: []extensioncore.VersionRange{{Name: extensioncore.ControlName, Major: 1, MinMinor: 0, MaxMinor: 0}}, Interfaces: []extensioncore.VersionRange{{Name: ExpectedInterface, Major: 1, MinMinor: 0, MaxMinor: 0}}, Capabilities: append([]string(nil), expectedCapabilities...), Secrets: []extensioncore.SecretRequirement{{ID: ExpectedSecretSlot, Required: true}}}
	return reflect.DeepEqual(m, expected)
}

func sortedSubset(values, allowed []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for i, value := range values {
		if i > 0 && value == values[i-1] {
			return false
		}
		at := sort.SearchStrings(allowed, value)
		if at == len(allowed) || allowed[at] != value {
			return false
		}
	}
	return true
}
func validUUID(v string) bool {
	return len(v) == 36 && v[8] == '-' && v[13] == '-' && v[18] == '-' && v[23] == '-' && validHex(strings.ReplaceAll(v, "-", ""), 32)
}
func validHex(v string, length int) bool {
	if len(v) != length {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func validZone(v string) bool {
	if len(v) < 1 || len(v) > 253 || strings.ToLower(v) != v || strings.HasSuffix(v, ".") || strings.Contains(v, "..") {
		return false
	}
	labels := strings.Split(v, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
