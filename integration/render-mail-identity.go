//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/gotthboard/gotth-stack/internal/composition/mailidentity"
	"github.com/gotthboard/gotth-stack/internal/extensions/godaddydns"
)

func main() {
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: go run ./integration/render-mail-identity.go <provider-artifact>")
		os.Exit(2)
	}
	artifact, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	request := godaddydns.Request{
		Distribution:  godaddydns.DistributionCandidate,
		ForgejoCommit: godaddydns.ExpectedSourceCommit,
		InstanceID:    "11111111-1111-1111-1111-111111111111",
		Capabilities:  []string{"dns.records.create", "dns.records.delete", "dns.records.observe", "dns.records.replace"},
		Zones:         []string{"example.test"},
		RecordTypes:   []string{"A", "AAAA", "MX", "SRV", "TXT"},
		Environment:   "ote", TimeoutSeconds: 15,
	}
	admission, err := godaddydns.VerifyAndAdmit(bytes.NewReader(artifact), request)
	if err != nil {
		fail(err)
	}
	result, err := mailidentity.Compose(mailidentity.Input{
		Environment: mailidentity.EnvironmentDisposable,
		Zone:        "example.test", WebHostname: "mail.example.test", IdentityHostname: "auth.example.test", MailHostname: "mx.example.test",
		IPv4: "192.0.2.10", IPv6: "2001:db8::10", ProductUpstream: "10.0.0.2:8080", AuthentikUpstream: "10.0.0.3:9000",
		OIDCClientSecretFile: "/run/secrets/gotth-mail-oidc-client", SCIMTokenFile: "/run/secrets/gotth-mail-scim-token",
		DKIMSelector: "mail", DKIMPublicKeyTXT: "v=DKIM1; k=rsa; p=QUJDRA==",
		DMARCReportAddress: "mailto:dmarc@example.test", TLSRPTReportAddress: "mailto:tlsrpt@example.test",
		DNSInstanceID: request.InstanceID, ProviderAdmission: admission,
	})
	if err != nil {
		fail(err)
	}
	if result.ProductionReady || len(result.Blockers) != 1 || result.Blockers[0].Code != "provider_publication_unavailable" {
		fail(fmt.Errorf("unexpected candidate readiness"))
	}
	if _, err := os.Stdout.Write(result.AuthentikBlueprint); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
