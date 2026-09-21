package godaddydns

import "errors"

const (
	ExpectedSourceCommit               = "c1f3525753da982a3ba84a5f443ae8e35dadebc8"
	ExpectedFoundationCommit           = "3822dd722bc8606b8843d0a68c7c3e9f598d9ccc"
	ExpectedVersion                    = "1.0.0-alpha.2"
	ExpectedArchiveSHA256              = "88ce5fb3c2bd2dc1f66707a8657886055632a31e0a4e7e1cec8e459e0742435f"
	ExpectedManifestSHA256             = "c0cf3c5921a692c54be1db4d734d7cacd3264f3bb6eb6778940e291e107b221f"
	ExpectedExecutableSHA256           = "5546b9f02286757ae7af70094bae1633e89d07355849a9da87885c2be1bb1c42"
	ExpectedConfigMetadataSHA256       = "fb0ed0362b5380ddaf5efd639a93a5880a59e97746ab8ee5afd8ba3d5cefe6e7"
	ExpectedLicenseSHA256              = "5d3e572b01d8ed617e25ece3838c73f277e3c9da35ec0b680d1871e9d6cf62f8"
	ExpectedExtensionID                = "gotth.dns.provider.godaddy"
	ExpectedInterface                  = "gotth.dns.records"
	ExpectedSecretSlot                 = "godaddy.pat"
	ForgejoRepository                  = "https://git.dannyhunn.com/gotthboard/gotth-extension-godaddy-dns"
	GitHubRepository                   = "https://github.com/gotthboard/gotth-extension-godaddy-dns"
	ConfigSchema                       = "gotth.extension.godaddy-dns.config.v1"
	BindingSchema                      = "gotth.extension.runtime-binding.v1"
	maxArchiveBytes              int64 = 32 << 20
	maxExpandedBytes             int64 = 64 << 20
)

var (
	ErrInvalidAdmission = errors.New("GoDaddy DNS extension admission is invalid")
	ErrInvalidArtifact  = errors.New("GoDaddy DNS extension artifact is invalid")

	expectedCapabilities = []string{"dns.records.create", "dns.records.delete", "dns.records.observe", "dns.records.replace"}
	expectedRecordTypes  = []string{"A", "AAAA", "CAA", "CNAME", "MX", "SRV", "TXT"}
)

type DistributionStatus string

const (
	DistributionCandidate DistributionStatus = "candidate"
	DistributionPublished DistributionStatus = "published"
)

type Request struct {
	Distribution   DistributionStatus
	ForgejoCommit  string
	GitHubCommit   string
	InstanceID     string
	Capabilities   []string
	Zones          []string
	RecordTypes    []string
	Environment    string
	TimeoutSeconds int
}

type Result struct {
	Executable            []byte
	RuntimeBinding        []byte
	ProviderConfiguration []byte
	ManifestSHA256        string
	GrantSHA256           string
	SessionSHA256         string
	AdmissionSHA256       string
	ReleaseReady          bool
	seal                  string
}

// Verified reports whether this result is still the exact output of
// VerifyAndAdmit. Mutating any exported evidence invalidates the private seal.
func (result Result) Verified() bool { return result.seal != "" && result.seal == resultSeal(result) }

type artifactPins struct {
	Archive, Manifest, Executable, ConfigMetadata, License string
	PublicationAvailable                                   bool
}

var expectedPins = artifactPins{Archive: ExpectedArchiveSHA256, Manifest: ExpectedManifestSHA256, Executable: ExpectedExecutableSHA256, ConfigMetadata: ExpectedConfigMetadataSHA256, License: ExpectedLicenseSHA256, PublicationAvailable: false}
