package godaddydns

import "errors"

const (
	ExpectedSourceCommit               = "b313b413dc9ea7d495fa45b21b26c6e3dc94b064"
	ExpectedFoundationCommit           = "3822dd722bc8606b8843d0a68c7c3e9f598d9ccc"
	ExpectedVersion                    = "1.0.0-alpha.1"
	ExpectedArchiveSHA256              = "03b532d557a9b2894e71ccb224f2d5cd18fcf07f15b94575e1ac48212c9129bd"
	ExpectedManifestSHA256             = "c765d2681e1104c597b64e49b0dc7fb226fcc9006f5958ccbbe4d5f81dec5532"
	ExpectedExecutableSHA256           = "d78e4a1f8175de68809210c270c17dab83d87b392c16e6b7e0618f45911e49f5"
	ExpectedConfigMetadataSHA256       = "ce53aa4df550439a30e1cd8263670e3d5f6ca74244593fbe36190f962f512eec"
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
	expectedRecordTypes  = []string{"A", "AAAA", "CAA", "CNAME", "MX", "TXT"}
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
}

type artifactPins struct {
	Archive, Manifest, Executable, ConfigMetadata, License string
	PublicationAvailable                                   bool
}

var expectedPins = artifactPins{Archive: ExpectedArchiveSHA256, Manifest: ExpectedManifestSHA256, Executable: ExpectedExecutableSHA256, ConfigMetadata: ExpectedConfigMetadataSHA256, License: ExpectedLicenseSHA256, PublicationAvailable: false}
