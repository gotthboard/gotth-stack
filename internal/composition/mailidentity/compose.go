// Package mailidentity renders one closed, secret-free desired state for the
// GOTTH Mail public, identity, certificate, and DNS boundaries.
package mailidentity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/netip"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	authentik "github.com/gotthboard/gotth-authentik/pkg/authentik"
	"github.com/gotthboard/gotth-stack/internal/extensions/godaddydns"
	"golang.org/x/net/publicsuffix"
)

type Environment string

const (
	EnvironmentDisposable Environment = "disposable"
	EnvironmentProduction Environment = "production"
)

var (
	ErrInvalidInput          = errors.New("mail identity composition input is invalid")
	ErrProviderAdmission     = errors.New("mail identity provider admission is invalid")
	ErrProductionUnavailable = errors.New("mail identity production composition is unavailable")
)

type Input struct {
	Environment                                       Environment
	Zone, WebHostname, IdentityHostname, MailHostname string
	IPv4, IPv6                                        string
	ProductUpstream, AuthentikUpstream                string
	DKIMSelector, DKIMPublicKeyTXT                    string
	DMARCReportAddress, TLSRPTReportAddress           string
	DNSInstanceID                                     string
	ProviderAdmission                                 godaddydns.Result
}

type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CertificateOwnership struct {
	Hostname  string   `json:"hostname"`
	Owner     string   `json:"owner"`
	Listeners []string `json:"listeners"`
}

type DNSRecord struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Data     string  `json:"data"`
	TTL      uint32  `json:"ttl"`
	Priority *uint32 `json:"priority,omitempty"`
	Service  string  `json:"service,omitempty"`
	Protocol string  `json:"protocol,omitempty"`
	Weight   *uint32 `json:"weight,omitempty"`
	Port     *uint32 `json:"port,omitempty"`
}

type Blocker struct {
	Code string `json:"code"`
}

type Digests struct {
	Caddyfile          string `json:"caddyfile"`
	AuthentikBlueprint string `json:"authentik_blueprint"`
	MailVariables      string `json:"mail_variables"`
	Certificates       string `json:"certificates"`
	Records            string `json:"records"`
	DNSRequest         string `json:"dns_request"`
	ProviderAdmission  string `json:"provider_admission"`
	ProviderGrant      string `json:"provider_grant"`
	ProviderSession    string `json:"provider_session"`
	Composition        string `json:"composition,omitempty"`
}

type Result struct {
	Caddyfile, AuthentikBlueprint []byte
	MailVariables                 []Variable
	Certificates                  []CertificateOwnership
	Records                       []DNSRecord
	DNSRequest                    godaddydns.Request
	Digests                       Digests
	ProductionReady               bool
	Blockers                      []Blocker
	seal                          string
}

// Verified reports whether this result is still the exact output of Compose.
// Mutating any exported desired state or digest invalidates the private seal.
func (result Result) Verified() bool { return result.seal != "" && result.seal == resultSeal(result) }

type admittedConfiguration struct {
	Schema         string   `json:"schema"`
	Environment    string   `json:"environment"`
	Zones          []string `json:"zones"`
	RecordTypes    []string `json:"record_types"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type admittedBinding struct {
	Schema         string   `json:"schema"`
	InstanceID     string   `json:"instance_id"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	GrantSHA256    string   `json:"grant_sha256"`
	SessionSHA256  string   `json:"session_sha256"`
	Capabilities   []string `json:"capabilities"`
}

var (
	documentationIPv4 = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24")}
	documentationIPv6 = []netip.Prefix{netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("3fff::/20")}
	nonPublicPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("100:0:0:1::/64"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("5f00::/16"), netip.MustParsePrefix("fc00::/7"),
		netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
	}
	providerCapabilities = []string{"dns.records.create", "dns.records.delete", "dns.records.observe", "dns.records.replace"}
	specialUseDomains    = []string{"alt", "example", "example.com", "example.net", "example.org", "invalid", "local", "localhost", "onion", "test"}
)

const (
	oidcClientSecretFile      = "/run/secrets/oidc-client-secret"
	scimTokenFile             = "/run/secrets/scim-client-token"
	maxProviderRecordDataSize = 512
	maxMailboxSize            = 254
	maxMailboxLocalPartSize   = 64
)

func Compose(input Input) (Result, error) {
	if input.Environment != EnvironmentDisposable && input.Environment != EnvironmentProduction {
		return Result{}, ErrInvalidInput
	}
	if !validZone(input.Zone) || !validHostname(input.WebHostname, input.Zone) || !validHostname(input.IdentityHostname, input.Zone) || !validHostname(input.MailHostname, input.Zone) {
		return Result{}, ErrInvalidInput
	}
	mtaHostname := "mta-sts." + input.Zone
	if !validHostname(mtaHostname, input.Zone) {
		return Result{}, ErrInvalidInput
	}
	hosts := []string{input.WebHostname, input.IdentityHostname, input.MailHostname, mtaHostname}
	if !uniqueStrings(hosts) || !validUpstream(input.ProductUpstream) || !validUpstream(input.AuthentikUpstream) || input.ProductUpstream == input.AuthentikUpstream {
		return Result{}, ErrInvalidInput
	}
	ipv4, err := validateAddress(input.IPv4, true, input.Environment)
	if err != nil {
		return Result{}, ErrInvalidInput
	}
	var ipv6 netip.Addr
	if input.IPv6 != "" {
		ipv6, err = validateAddress(input.IPv6, false, input.Environment)
		if err != nil {
			return Result{}, ErrInvalidInput
		}
	}
	if (input.Environment == EnvironmentDisposable && !strings.HasSuffix(input.Zone, ".test")) ||
		(input.Environment == EnvironmentProduction && !publicProductionZone(input.Zone)) {
		return Result{}, ErrInvalidInput
	}
	if !validSelector(input.DKIMSelector) || !validDKIM(input.DKIMPublicKeyTXT) || !validReportAddress(input.DMARCReportAddress, input.Zone) || !validReportAddress(input.TLSRPTReportAddress, input.Zone) {
		return Result{}, ErrInvalidInput
	}

	providerTypes := []string{"A", "MX", "SRV", "TXT"}
	if ipv6.IsValid() {
		providerTypes = []string{"A", "AAAA", "MX", "SRV", "TXT"}
	}
	providerEnvironment := "ote"
	if input.Environment == EnvironmentProduction {
		providerEnvironment = "production"
	}
	configuration, binding, err := validateAdmission(input.ProviderAdmission, input.DNSInstanceID, input.Zone, providerEnvironment, providerTypes)
	if err != nil {
		return Result{}, err
	}
	if input.Environment == EnvironmentProduction && !input.ProviderAdmission.ReleaseReady {
		return Result{}, ErrProductionUnavailable
	}
	evidence := providerEvidence{Admission: input.ProviderAdmission.AdmissionSHA256, Grant: input.ProviderAdmission.GrantSHA256, Session: input.ProviderAdmission.SessionSHA256, ReleaseReady: input.ProviderAdmission.ReleaseReady}
	return composeVerified(input, mtaHostname, ipv4, ipv6, configuration, binding, evidence)
}

type providerEvidence struct {
	Admission, Grant, Session string
	ReleaseReady              bool
}

func composeVerified(input Input, mtaHostname string, ipv4, ipv6 netip.Addr, configuration admittedConfiguration, binding admittedBinding, evidence providerEvidence) (Result, error) {
	issuer := "https://" + input.IdentityHostname + "/application/o/gotth-mail/"
	callback := "https://" + input.WebHostname + "/api/v1/oidc/callback"
	scimURL := "https://" + input.WebHostname + "/scim/v2"
	deployment := authentik.Deployment{
		Application: authentik.Application{
			Slug: "gotth-mail", Name: "GOTTH Mail", RegistrationTitle: "Register for GOTTH Mail",
			UserPath: "users/gotth-mail", AccessGroup: "gotth-mail-users", LaunchURL: "https://" + input.WebHostname + "/",
			Provider: authentik.Provider{Name: "gotth-mail-oidc", ClientType: "confidential", ClientID: "gotth-mail", RedirectURIs: []string{callback}, AuthorizationFlow: "default-provider-authorization-implicit-consent", InvalidationFlow: "default-provider-invalidation-flow", SigningKey: "authentik Self-signed Certificate"},
		},
		OIDCClientSecretFile: oidcClientSecretFile,
		SCIM:                 authentik.SCIMBackchannel{Name: "gotth-mail-scim", URL: scimURL, TokenFile: scimTokenFile},
	}
	blueprint, err := authentik.RenderDeploymentBlueprint(deployment)
	if err != nil {
		return Result{}, ErrInvalidInput
	}

	policy := "version: STSv1\nmode: enforce\nmx: " + input.MailHostname + "\nmax_age: 86400\n"
	caddyfile := []byte(fmt.Sprintf("{\n\tadmin off\n\tservers {\n\t\tprotocols h1 h2\n\t}\n}\n\n%s {\n\treverse_proxy %s\n}\n\n%s {\n\treverse_proxy %s\n}\n\n%s {\n\t@policy path /.well-known/mta-sts.txt\n\thandle @policy {\n\t\theader Content-Type \"text/plain; charset=utf-8\"\n\t\trespond \"version: STSv1\\nmode: enforce\\nmx: %s\\nmax_age: 86400\\n\" 200\n\t}\n\trespond 404\n}\n", input.WebHostname, input.ProductUpstream, input.IdentityHostname, input.AuthentikUpstream, mtaHostname, input.MailHostname))

	variables := []Variable{
		{"GOTTH_MAIL_AUTHENTIK_CLIENT_ID", "gotth-mail"},
		{"GOTTH_MAIL_AUTHENTIK_CLIENT_SECRET_FILE", oidcClientSecretFile},
		{"GOTTH_MAIL_AUTHENTIK_ISSUER", issuer},
		{"GOTTH_MAIL_AUTHENTIK_REDIRECT_URI", callback},
		{"GOTTH_MAIL_SCIM_EXTERNAL_URL", scimURL},
	}
	certificates := []CertificateOwnership{
		{input.IdentityHostname, "caddy", []string{"tcp/80", "tcp/443"}},
		{mtaHostname, "caddy", []string{"tcp/80", "tcp/443"}},
		{input.WebHostname, "caddy", []string{"tcp/80", "tcp/443"}},
		{input.MailHostname, "mail-front", []string{"tcp/25", "tcp/143", "tcp/465", "tcp/587", "tcp/993"}},
	}
	if err := validateOwnership(certificates); err != nil {
		return Result{}, ErrInvalidInput
	}
	records := buildRecords(input, mtaHostname, policy, ipv4, ipv6)
	if !canonicalRecords(records, input.Zone) {
		return Result{}, ErrInvalidInput
	}

	distribution := godaddydns.DistributionCandidate
	githubCommit := ""
	blockers := []Blocker{{Code: "authentik_blueprint_apply_unavailable"}, {Code: "ptr_authority_unverified"}, {Code: "provider_publication_unavailable"}}
	if evidence.ReleaseReady {
		distribution = godaddydns.DistributionPublished
		githubCommit = godaddydns.ExpectedSourceCommit
		blockers = blockers[:2]
	}
	dnsRequest := godaddydns.Request{Distribution: distribution, ForgejoCommit: godaddydns.ExpectedSourceCommit, GitHubCommit: githubCommit, InstanceID: binding.InstanceID, Capabilities: append([]string(nil), providerCapabilities...), Zones: append([]string(nil), configuration.Zones...), RecordTypes: append([]string(nil), configuration.RecordTypes...), Environment: configuration.Environment, TimeoutSeconds: configuration.TimeoutSeconds}

	result := Result{Caddyfile: caddyfile, AuthentikBlueprint: blueprint, MailVariables: variables, Certificates: certificates, Records: records, DNSRequest: dnsRequest, ProductionReady: false, Blockers: blockers}
	result.Digests = Digests{
		Caddyfile: digest(caddyfile), AuthentikBlueprint: digest(blueprint),
		MailVariables: digestJSON(variables), Certificates: digestJSON(certificates), Records: digestJSON(records), DNSRequest: digestJSON(dnsRequest),
		ProviderAdmission: evidence.Admission, ProviderGrant: evidence.Grant, ProviderSession: evidence.Session,
	}
	wire := struct {
		Schema          string    `json:"schema"`
		Environment     string    `json:"environment"`
		Zone            string    `json:"zone"`
		Web             string    `json:"web_hostname"`
		Identity        string    `json:"identity_hostname"`
		Mail            string    `json:"mail_hostname"`
		Digests         Digests   `json:"digests"`
		ProductionReady bool      `json:"production_ready"`
		Blockers        []Blocker `json:"blockers,omitempty"`
	}{"gotth.stack.mail-identity-composition.v1", string(input.Environment), input.Zone, input.WebHostname, input.IdentityHostname, input.MailHostname, result.Digests, result.ProductionReady, result.Blockers}
	result.Digests.Composition = digestJSON(wire)
	result.seal = resultSeal(result)
	return result, nil
}

func validateAdmission(result godaddydns.Result, instanceID, zone, environment string, recordTypes []string) (admittedConfiguration, admittedBinding, error) {
	if !result.Verified() {
		return admittedConfiguration{}, admittedBinding{}, ErrProviderAdmission
	}
	var configuration admittedConfiguration
	var binding admittedBinding
	if err := strictJSON(result.ProviderConfiguration, &configuration); err != nil {
		return admittedConfiguration{}, admittedBinding{}, ErrProviderAdmission
	}
	if err := strictJSON(result.RuntimeBinding, &binding); err != nil {
		return admittedConfiguration{}, admittedBinding{}, ErrProviderAdmission
	}
	if configuration.Schema != godaddydns.ConfigSchema || configuration.Environment != environment || configuration.TimeoutSeconds != 15 || !equalStrings(configuration.Zones, []string{zone}) || !equalStrings(configuration.RecordTypes, recordTypes) {
		return admittedConfiguration{}, admittedBinding{}, ErrProviderAdmission
	}
	if binding.Schema != godaddydns.BindingSchema || binding.InstanceID != instanceID || binding.ManifestSHA256 != result.ManifestSHA256 || binding.GrantSHA256 != result.GrantSHA256 || binding.SessionSHA256 != result.SessionSHA256 || !equalStrings(binding.Capabilities, providerCapabilities) {
		return admittedConfiguration{}, admittedBinding{}, ErrProviderAdmission
	}
	return configuration, binding, nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrProviderAdmission
	}
	encoded, err := json.Marshal(target)
	if err != nil || string(encoded) != string(data) {
		return ErrProviderAdmission
	}
	return nil
}

func buildRecords(input Input, mtaHostname, policy string, ipv4, ipv6 netip.Addr) []DNSRecord {
	const ttl = uint32(600)
	hosts := []string{input.WebHostname, input.IdentityHostname, input.MailHostname, mtaHostname}
	records := make([]DNSRecord, 0, 14)
	for _, host := range hosts {
		records = append(records, DNSRecord{Name: relativeName(host, input.Zone), Type: "A", Data: ipv4.String(), TTL: ttl})
		if ipv6.IsValid() {
			records = append(records, DNSRecord{Name: relativeName(host, input.Zone), Type: "AAAA", Data: ipv6.String(), TTL: ttl})
		}
	}
	priority, weight, port := uint32(10), uint32(0), uint32(587)
	records = append(records,
		DNSRecord{Name: "@", Type: "MX", Data: input.MailHostname + ".", TTL: ttl, Priority: &priority},
		DNSRecord{Name: "@", Type: "TXT", Data: "v=spf1 mx -all", TTL: ttl},
		DNSRecord{Name: input.DKIMSelector + "._domainkey", Type: "TXT", Data: input.DKIMPublicKeyTXT, TTL: ttl},
		DNSRecord{Name: "_dmarc", Type: "TXT", Data: "v=DMARC1; p=reject; rua=" + input.DMARCReportAddress, TTL: ttl},
		DNSRecord{Name: "_mta-sts", Type: "TXT", Data: "v=STSv1; id=" + digest([]byte(policy))[:16], TTL: ttl},
		DNSRecord{Name: "_smtp._tls", Type: "TXT", Data: "v=TLSRPTv1; rua=" + input.TLSRPTReportAddress, TTL: ttl},
		DNSRecord{Name: "_submission._tcp", Type: "SRV", Data: input.MailHostname + ".", TTL: ttl, Priority: &priority, Service: "_submission", Protocol: "_tcp", Weight: &weight, Port: &port},
	)
	sort.Slice(records, func(i, j int) bool { return recordKey(records[i]) < recordKey(records[j]) })
	return records
}

func canonicalRecords(records []DNSRecord, zone string) bool {
	for index, record := range records {
		if len(record.Data) == 0 || len(record.Data) > maxProviderRecordDataSize ||
			record.Name != "@" && len(record.Name)+1+len(zone) > 253 {
			return false
		}
		if index > 0 && recordKey(records[index-1]) == recordKey(record) {
			return false
		}
	}
	return true
}

func recordKey(record DNSRecord) string {
	encoded, _ := json.Marshal(record)
	return record.Type + "\x00" + record.Name + "\x00" + string(encoded)
}
func relativeName(host, zone string) string { return strings.TrimSuffix(host, "."+zone) }

func validateOwnership(entries []CertificateOwnership) error {
	hostOwner := map[string]string{}
	listenerOwner := map[string]string{}
	for _, entry := range entries {
		if owner, ok := hostOwner[entry.Hostname]; ok && owner != entry.Owner {
			return ErrInvalidInput
		}
		hostOwner[entry.Hostname] = entry.Owner
		for _, listener := range entry.Listeners {
			if owner, ok := listenerOwner[listener]; ok && owner != entry.Owner {
				return ErrInvalidInput
			}
			listenerOwner[listener] = entry.Owner
		}
	}
	return nil
}

func validateAddress(raw string, ipv4 bool, environment Environment) (netip.Addr, error) {
	address, err := netip.ParseAddr(raw)
	if err != nil || address.String() != raw || address.Is4() != ipv4 || address.Is4In6() || address.Zone() != "" {
		return netip.Addr{}, ErrInvalidInput
	}
	documentation := isDocumentation(address)
	if (environment == EnvironmentDisposable && !documentation) ||
		(environment == EnvironmentProduction && (!publicAddress(address) || documentation)) {
		return netip.Addr{}, ErrInvalidInput
	}
	return address, nil
}

func publicAddress(address netip.Addr) bool {
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func isDocumentation(address netip.Addr) bool {
	if address.Is4() {
		for _, prefix := range documentationIPv4 {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	for _, prefix := range documentationIPv6 {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func validUpstream(raw string) bool {
	endpoint, err := netip.ParseAddrPort(raw)
	return err == nil && endpoint.String() == raw && endpoint.Port() != 0 && (endpoint.Addr().IsPrivate() || endpoint.Addr().IsLoopback())
}

func validZone(value string) bool {
	if len(value) < 3 || len(value) > 253 || value != strings.ToLower(value) || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

func validHostname(value, zone string) bool {
	return validZone(value) && strings.HasSuffix(value, "."+zone)
}

func validLabel(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func publicProductionZone(zone string) bool {
	suffix, icann := publicsuffix.PublicSuffix(zone)
	if !icann || suffix == zone || zone == "arpa" || strings.HasSuffix(zone, ".arpa") || hasSpecialUseSuffix(zone) {
		return false
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(zone)
	return err == nil && registrable != ""
}

func hasSpecialUseSuffix(zone string) bool {
	for _, reserved := range specialUseDomains {
		if zone == reserved || strings.HasSuffix(zone, "."+reserved) {
			return true
		}
	}
	return false
}

func validSelector(value string) bool { return validLabel(value) }

func validDKIM(value string) bool {
	const prefix = "v=DKIM1; k=rsa; p="
	if len(value) <= len(prefix) || len(value) > 512 || !strings.HasPrefix(value, prefix) || !utf8.ValidString(value) {
		return false
	}
	encoded := value[len(prefix):]
	for _, character := range encoded {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '+' || character == '/' || character == '=') {
			return false
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && len(decoded) > 0 && base64.StdEncoding.EncodeToString(decoded) == encoded
}

func validReportAddress(value, zone string) bool {
	if !strings.HasPrefix(value, "mailto:") || value != strings.ToLower(value) || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	address := strings.TrimPrefix(value, "mailto:")
	if len(address) == 0 || len(address) > maxMailboxSize {
		return false
	}
	parsed, err := mail.ParseAddress(address)
	parts := strings.Split(address, "@")
	return err == nil && parsed.Address == address && len(parts) == 2 && validReportLocalPart(parts[0]) && parts[1] == zone
}

func validReportLocalPart(value string) bool {
	if value == "" || len(value) > maxMailboxLocalPartSize || value[0] == '.' || value[len(value)-1] == '.' || strings.Contains(value, "..") {
		return false
	}
	for _, character := range []byte(value) {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || strings.ContainsRune("!$'*+-._~", rune(character))) {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func resultSeal(result Result) string {
	wire := struct {
		Caddyfile          []byte                 `json:"caddyfile"`
		AuthentikBlueprint []byte                 `json:"authentik_blueprint"`
		MailVariables      []Variable             `json:"mail_variables"`
		Certificates       []CertificateOwnership `json:"certificates"`
		Records            []DNSRecord            `json:"records"`
		DNSRequest         godaddydns.Request     `json:"dns_request"`
		Digests            Digests                `json:"digests"`
		ProductionReady    bool                   `json:"production_ready"`
		Blockers           []Blocker              `json:"blockers"`
	}{result.Caddyfile, result.AuthentikBlueprint, result.MailVariables, result.Certificates, result.Records, result.DNSRequest, result.Digests, result.ProductionReady, result.Blockers}
	return digestJSON(wire)
}

func digest(value []byte) string  { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func digestJSON(value any) string { encoded, _ := json.Marshal(value); return digest(encoded) }
