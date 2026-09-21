package mailidentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/gotthboard/gotth-stack/internal/extensions/godaddydns"
)

type marshalFailure struct{}

func (*marshalFailure) UnmarshalJSON([]byte) error { return nil }
func (*marshalFailure) MarshalJSON() ([]byte, error) {
	return nil, errors.New("deliberate marshal failure")
}

func validInput() Input {
	return Input{
		Environment: EnvironmentDisposable, Zone: "example.test",
		WebHostname: "mail.example.test", IdentityHostname: "auth.example.test", MailHostname: "mx.example.test",
		IPv4: "192.0.2.10", IPv6: "2001:db8::10", ProductUpstream: "10.0.0.2:8080", AuthentikUpstream: "10.0.0.3:9000",
		DKIMSelector: "mail", DKIMPublicKeyTXT: "v=DKIM1; k=rsa; p=QUJDRA==",
		DMARCReportAddress: "mailto:dmarc@example.test", TLSRPTReportAddress: "mailto:tlsrpt@example.test",
		DNSInstanceID: "11111111-1111-1111-1111-111111111111",
	}
}

func candidateConfiguration() admittedConfiguration {
	return admittedConfiguration{Schema: godaddydns.ConfigSchema, Environment: "ote", Zones: []string{"example.test"}, RecordTypes: []string{"A", "AAAA", "MX", "SRV", "TXT"}, TimeoutSeconds: 15}
}

func candidateBinding() admittedBinding {
	return admittedBinding{Schema: godaddydns.BindingSchema, InstanceID: "11111111-1111-1111-1111-111111111111", ManifestSHA256: strings.Repeat("d", 64), GrantSHA256: strings.Repeat("b", 64), SessionSHA256: strings.Repeat("c", 64), Capabilities: append([]string(nil), providerCapabilities...)}
}

func TestComposeVerifiedDeterministicDesiredState(t *testing.T) {
	input := validInput()
	configuration, binding := candidateConfiguration(), candidateBinding()
	evidence := providerEvidence{Admission: strings.Repeat("a", 64), Grant: strings.Repeat("b", 64), Session: strings.Repeat("c", 64)}
	first, err := composeVerified(input, "mta-sts.example.test", mustAddr(t, input.IPv4), mustAddr(t, input.IPv6), configuration, binding, evidence)
	if err != nil {
		t.Fatal(err)
	}
	second, err := composeVerified(input, "mta-sts.example.test", mustAddr(t, input.IPv4), mustAddr(t, input.IPv6), configuration, binding, evidence)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("nondeterministic result err=%v", err)
	}
	if !first.Verified() || first.ProductionReady || !reflect.DeepEqual(first.Blockers, []Blocker{{"authentik_blueprint_apply_unavailable"}, {"ptr_authority_unverified"}, {"provider_publication_unavailable"}}) || first.Digests.Composition == "" {
		t.Fatalf("candidate readiness=%#v", first)
	}
	blueprint := string(first.AuthentikBlueprint)
	for _, required := range []string{"gotth-mail-oidc", "gotth-mail-users", "gotth-mail-scim", "!File \"/run/secrets/oidc-client-secret\"", "!File \"/run/secrets/scim-client-token\""} {
		if !strings.Contains(blueprint, required) {
			t.Fatalf("blueprint missing %q", required)
		}
	}
	for _, required := range []string{"protocols h1 h2", "mail.example.test", "auth.example.test", "mta-sts.example.test", "/.well-known/mta-sts.txt", "mx: mx.example.test"} {
		if !bytes.Contains(first.Caddyfile, []byte(required)) {
			t.Fatalf("Caddyfile missing %q", required)
		}
	}
	wantVariables := []Variable{
		{"GOTTH_MAIL_AUTHENTIK_CLIENT_ID", "gotth-mail"},
		{"GOTTH_MAIL_AUTHENTIK_CLIENT_SECRET_FILE", "/run/secrets/oidc-client-secret"},
		{"GOTTH_MAIL_AUTHENTIK_ISSUER", "https://auth.example.test/application/o/gotth-mail/"},
		{"GOTTH_MAIL_AUTHENTIK_REDIRECT_URI", "https://mail.example.test/api/v1/oidc/callback"},
		{"GOTTH_MAIL_SCIM_EXTERNAL_URL", "https://mail.example.test/scim/v2"},
	}
	if !reflect.DeepEqual(first.MailVariables, wantVariables) {
		t.Fatalf("variables=%#v", first.MailVariables)
	}
	assertRecord(t, first.Records, DNSRecord{Name: "_submission._tcp", Type: "SRV", Data: "mx.example.test.", TTL: 600, Priority: uint32Pointer(10), Service: "_submission", Protocol: "_tcp", Weight: uint32Pointer(0), Port: uint32Pointer(587)})
	assertRecord(t, first.Records, DNSRecord{Name: "@", Type: "MX", Data: "mx.example.test.", TTL: 600, Priority: uint32Pointer(10)})
	if !reflect.DeepEqual(first.DNSRequest.RecordTypes, configuration.RecordTypes) || first.DNSRequest.Distribution != godaddydns.DistributionCandidate || first.DNSRequest.ForgejoCommit != godaddydns.ExpectedSourceCommit {
		t.Fatalf("DNS request=%#v", first.DNSRequest)
	}
}

func TestComposeVerifiedPublishedAndIPv4OnlyStates(t *testing.T) {
	input := validInput()
	input.Environment = EnvironmentProduction
	input.Zone = "gotthmail.net"
	input.WebHostname = "mail.gotthmail.net"
	input.IdentityHostname = "auth.gotthmail.net"
	input.MailHostname = "mx.gotthmail.net"
	input.IPv4 = "8.8.8.8"
	input.IPv6 = ""
	input.DMARCReportAddress = "mailto:dmarc@gotthmail.net"
	input.TLSRPTReportAddress = "mailto:tlsrpt@gotthmail.net"
	configuration := admittedConfiguration{Schema: godaddydns.ConfigSchema, Environment: "production", Zones: []string{input.Zone}, RecordTypes: []string{"A", "MX", "SRV", "TXT"}, TimeoutSeconds: 15}
	result, err := composeVerified(input, "mta-sts.gotthmail.net", mustAddr(t, input.IPv4), netip.Addr{}, configuration, candidateBinding(), providerEvidence{Admission: strings.Repeat("a", 64), Grant: strings.Repeat("b", 64), Session: strings.Repeat("c", 64), ReleaseReady: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified() || result.ProductionReady || !reflect.DeepEqual(result.Blockers, []Blocker{{"authentik_blueprint_apply_unavailable"}, {"ptr_authority_unverified"}}) || result.DNSRequest.Distribution != godaddydns.DistributionPublished || result.DNSRequest.GitHubCommit != godaddydns.ExpectedSourceCommit {
		t.Fatalf("published result=%#v", result)
	}
	for _, record := range result.Records {
		if record.Type == "AAAA" {
			t.Fatalf("unexpected IPv6 record: %#v", record)
		}
	}
}

func TestComposeRejectsUnverifiedAdmissionAndInvalidInputs(t *testing.T) {
	input := validInput()
	if _, err := Compose(input); !errors.Is(err, ErrProviderAdmission) {
		t.Fatalf("unverified admission err=%v", err)
	}
	invalid := []func(*Input){
		func(value *Input) { value.Environment = "custom" },
		func(value *Input) { value.Zone = "EXAMPLE.test" },
		func(value *Input) { value.WebHostname = value.IdentityHostname },
		func(value *Input) { value.WebHostname = "outside.test" },
		func(value *Input) { value.IPv4 = "10.0.0.1" },
		func(value *Input) { value.IPv6 = "2001:0db8::10" },
		func(value *Input) { value.ProductUpstream = "public.example:8080" },
		func(value *Input) { value.ProductUpstream = value.AuthentikUpstream },
		func(value *Input) { value.DKIMPublicKeyTXT = "secret\nvalue" },
		func(value *Input) { value.DKIMPublicKeyTXT = "v=DKIM1; k=rsa; p=A=A" },
		func(value *Input) { value.DKIMPublicKeyTXT = "v=DKIM1; k=rsa; p=" + strings.Repeat("A", 496) },
		func(value *Input) { value.DMARCReportAddress = "mailto:dmarc@other.test" },
	}
	for index, mutate := range invalid {
		candidate := validInput()
		mutate(&candidate)
		if _, err := Compose(candidate); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid %d err=%v", index, err)
		}
	}
}

func TestProductionAddressAndReservedZoneValidation(t *testing.T) {
	for _, raw := range []string{"10.0.0.1", "100.64.0.1", "192.0.0.1", "198.18.0.1", "203.0.113.1", "224.0.0.1", "64:ff9b::1", "2001::1", "2001:db8::1", "2002::1", "3fff::1", "fc00::1"} {
		ipv4 := !strings.Contains(raw, ":")
		if _, err := validateAddress(raw, ipv4, EnvironmentProduction); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("non-public production address %q err=%v", raw, err)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		ipv4 := !strings.Contains(raw, ":")
		if _, err := validateAddress(raw, ipv4, EnvironmentProduction); err != nil {
			t.Errorf("public production address %q err=%v", raw, err)
		}
	}
	if _, err := validateAddress("3fff::1", false, EnvironmentDisposable); err != nil {
		t.Fatalf("RFC 9637 documentation address rejected in disposable mode: %v", err)
	}
	for _, zone := range []string{"example.com", "mail.example.com", "example.net", "example.org", "mail.test", "mail.invalid", "mail.example", "mail.localhost", "mail.local"} {
		if !reservedProductionZone(zone) {
			t.Errorf("reserved production zone %q accepted", zone)
		}
	}
}

func TestClosedHelpersRejectAmbiguity(t *testing.T) {
	if canonicalRecords([]DNSRecord{{Name: "@", Type: "A", Data: "192.0.2.1", TTL: 600}, {Name: "@", Type: "A", Data: "192.0.2.1", TTL: 600}}) {
		t.Fatal("duplicate records accepted")
	}
	if !canonicalRecords(nil) {
		t.Fatal("empty canonical record set rejected")
	}
	for index, entries := range [][]CertificateOwnership{
		{{Hostname: "mail.example.test", Owner: "first"}, {Hostname: "mail.example.test", Owner: "second"}},
		{{Hostname: "mail.example.test", Owner: "first", Listeners: []string{"tcp/443"}}, {Hostname: "auth.example.test", Owner: "second", Listeners: []string{"tcp/443"}}},
	} {
		if err := validateOwnership(entries); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ownership conflict %d err=%v", index, err)
		}
	}
	for _, value := range []string{"a", "a..test", "-a.test", "a-.test", "a_.test", strings.Repeat("a", 64) + ".test"} {
		if validZone(value) {
			t.Errorf("invalid zone %q accepted", value)
		}
	}
}

func TestStrictJSONCanonicalBoundary(t *testing.T) {
	type value struct {
		Name string `json:"name"`
	}
	for index, raw := range []string{`{"name":"ok"} {}`, `{"unknown":true}`, ` {"name":"ok"}`, `{"name":"ok"}\n`} {
		var target value
		if err := strictJSON([]byte(raw), &target); err == nil {
			t.Errorf("noncanonical JSON %d err=%v", index, err)
		}
	}
	var failing marshalFailure
	if err := strictJSON([]byte(`{}`), &failing); !errors.Is(err, ErrProviderAdmission) {
		t.Fatalf("marshal failure err=%v", err)
	}
}

func TestResultSealRejectsMutation(t *testing.T) {
	input := validInput()
	result, err := composeVerified(input, "mta-sts.example.test", mustAddr(t, input.IPv4), mustAddr(t, input.IPv6), candidateConfiguration(), candidateBinding(), providerEvidence{Admission: strings.Repeat("a", 64), Grant: strings.Repeat("b", 64), Session: strings.Repeat("c", 64)})
	if err != nil || !result.Verified() {
		t.Fatalf("valid result err=%v verified=%v", err, result.Verified())
	}
	mutations := []func(Result) Result{
		func(value Result) Result {
			value.Caddyfile = append([]byte(nil), value.Caddyfile...)
			value.Caddyfile[0] ^= 1
			return value
		},
		func(value Result) Result {
			value.Records = append([]DNSRecord(nil), value.Records...)
			value.Records[0].Data = "203.0.113.99"
			return value
		},
		func(value Result) Result {
			value.DNSRequest.Zones = append([]string(nil), value.DNSRequest.Zones...)
			value.DNSRequest.Zones[0] = "other.test"
			return value
		},
		func(value Result) Result { value.Digests.Caddyfile = strings.Repeat("0", 64); return value },
		func(value Result) Result { value.Blockers = nil; return value },
	}
	for index, mutate := range mutations {
		if mutate(result).Verified() {
			t.Errorf("mutated result %d retained verification", index)
		}
	}
}

func TestGeneratedCaddyfileAdaptsAndValidates(t *testing.T) {
	caddy, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("caddy executable is unavailable")
	}
	input := validInput()
	result, err := composeVerified(input, "mta-sts.example.test", mustAddr(t, input.IPv4), mustAddr(t, input.IPv6), candidateConfiguration(), candidateBinding(), providerEvidence{Admission: strings.Repeat("a", 64), Grant: strings.Repeat("b", 64), Session: strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(caddy, "adapt", "--config", "-", "--adapter", "caddyfile", "--validate")
	command.Stdin = bytes.NewReader(result.Caddyfile)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("caddy adapt/validate: %v\n%s", err, stderr.Bytes())
	}
	var adapted struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Protocols []string `json:"protocols"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &adapted); err != nil {
		t.Fatal(err)
	}
	if len(adapted.Apps.HTTP.Servers) == 0 {
		t.Fatal("adapted Caddy configuration has no HTTP servers")
	}
	for name, server := range adapted.Apps.HTTP.Servers {
		if !reflect.DeepEqual(server.Protocols, []string{"h1", "h2"}) {
			t.Errorf("server %s protocols=%v", name, server.Protocols)
		}
	}
}

func TestComposeExactProviderArtifact(t *testing.T) {
	artifactPath := os.Getenv("GOTTH_GODADDY_ARTIFACT")
	if artifactPath == "" {
		t.Skip("GOTTH_GODADDY_ARTIFACT is required for exact provider composition")
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	input := validInput()
	request := godaddydns.Request{Distribution: godaddydns.DistributionCandidate, ForgejoCommit: godaddydns.ExpectedSourceCommit, InstanceID: input.DNSInstanceID, Capabilities: append([]string(nil), providerCapabilities...), Zones: []string{input.Zone}, RecordTypes: []string{"A", "AAAA", "MX", "SRV", "TXT"}, Environment: "ote", TimeoutSeconds: 15}
	admission, err := godaddydns.VerifyAndAdmit(bytes.NewReader(artifact), request)
	if err != nil {
		t.Fatal(err)
	}
	input.ProviderAdmission = admission
	result, err := Compose(input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified() || result.ProductionReady || result.Digests.ProviderAdmission != admission.AdmissionSHA256 || result.DNSRequest.RecordTypes[3] != "SRV" {
		t.Fatalf("composition=%#v", result)
	}
	if _, _, err := validateAdmission(admission, input.DNSInstanceID, input.Zone, "ote", request.RecordTypes); err != nil {
		t.Fatalf("valid admission rejected: %v", err)
	}
	for index, call := range []func() error{
		func() error {
			_, _, err := validateAdmission(admission, input.DNSInstanceID, "other.test", "ote", request.RecordTypes)
			return err
		},
		func() error {
			_, _, err := validateAdmission(admission, input.DNSInstanceID, input.Zone, "production", request.RecordTypes)
			return err
		},
		func() error {
			_, _, err := validateAdmission(admission, input.DNSInstanceID, input.Zone, "ote", []string{"A"})
			return err
		},
		func() error {
			_, _, err := validateAdmission(admission, "22222222-2222-2222-2222-222222222222", input.Zone, "ote", request.RecordTypes)
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrProviderAdmission) {
			t.Errorf("admission mismatch %d err=%v", index, err)
		}
	}
	admission.ProviderConfiguration = append([]byte(nil), admission.ProviderConfiguration...)
	admission.ProviderConfiguration[0] ^= 1
	input.ProviderAdmission = admission
	if _, err := Compose(input); !errors.Is(err, ErrProviderAdmission) {
		t.Fatalf("tampered admission err=%v", err)
	}

	production := validInput()
	production.Environment = EnvironmentProduction
	production.Zone = "gotthmail.net"
	production.WebHostname = "mail.gotthmail.net"
	production.IdentityHostname = "auth.gotthmail.net"
	production.MailHostname = "mx.gotthmail.net"
	production.IPv4 = "8.8.8.8"
	production.IPv6 = "2606:4700:4700::1111"
	production.DMARCReportAddress = "mailto:dmarc@gotthmail.net"
	production.TLSRPTReportAddress = "mailto:tlsrpt@gotthmail.net"
	productionRequest := request
	productionRequest.Zones = []string{production.Zone}
	productionRequest.Environment = "production"
	productionAdmission, err := godaddydns.VerifyAndAdmit(bytes.NewReader(artifact), productionRequest)
	if err != nil {
		t.Fatal(err)
	}
	production.ProviderAdmission = productionAdmission
	if _, err := Compose(production); !errors.Is(err, ErrProductionUnavailable) {
		t.Fatalf("candidate artifact production err=%v", err)
	}
}

func assertRecord(t *testing.T, records []DNSRecord, want DNSRecord) {
	t.Helper()
	for _, record := range records {
		if reflect.DeepEqual(record, want) {
			return
		}
	}
	t.Fatalf("missing record %#v in %#v", want, records)
}

func mustAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func uint32Pointer(value uint32) *uint32 { return &value }
