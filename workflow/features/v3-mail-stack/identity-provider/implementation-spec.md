# Implementation specification: mail identity/provider composition

## API shape

Add a closed package `internal/composition/mailidentity` with:

```go
type Environment string // disposable or production

type Input struct {
    Environment Environment
    Zone, WebHostname, IdentityHostname, MailHostname string
    IPv4, IPv6 string
    ProductUpstream, AuthentikUpstream string
    OIDCClientSecretFile, SCIMTokenFile string
    DKIMSelector, DKIMPublicKeyTXT string
    DMARCReportAddress, TLSRPTReportAddress string
    DNSInstanceID string
    ProviderAdmission godaddydns.Result
}

type Result struct {
    Caddyfile, AuthentikBlueprint []byte
    MailVariables []Variable
    Certificates []CertificateOwnership
    Records []DNSRecord
    DNSRequest godaddydns.Request
    Digests Digests
    ProductionReady bool
    Blockers []Blocker
}

func Compose(Input) (Result, error)
```

There is no options map, arbitrary site, arbitrary record, arbitrary Caddy
directive, environment-variable passthrough, apply method, or secret-value
field.

`godaddydns.Result` carries an unexported verification marker set only by
`VerifyAndAdmit`. Composition rejects zero-value or caller-constructed results,
parses and compares the admitted canonical provider configuration, and binds
the admission, manifest, grant, and session digests. Production additionally
requires the verified result's compiled `ReleaseReady` state.

## Validation

Canonical lowercase ASCII/punycode hostnames must lie inside the exact zone
and be mutually distinct. Production requires public hostnames and at least one
canonical public IP; disposable mode admits `.test` plus documentation IPs.
Upstreams are canonical `host:port` values whose hosts are literal loopback or
private IP addresses. Secret references reuse the same strict contract as the
pinned Authentik library and must be distinct.

DKIM is a bounded exact TXT value. Report destinations are exact `mailto:`
addresses inside the zone. TTL is fixed at 600 seconds. Duplicate logical
records reject before rendering.

## Fixed desired state

The Authentik application is fixed to slug/client ID `gotth-mail`, provider
`gotth-mail-oidc`, access group `gotth-mail-users`, user path
`users/gotth-mail`, default authorization/invalidation flows, and the named
Authentik signing key already used by the product profile. The SCIM provider
name is `gotth-mail-scim`.

Mail variables are sorted by name and limited to the five documented
`GOTTH_MAIL_AUTHENTIK_*`/`GOTTH_MAIL_SCIM_EXTERNAL_URL` keys. Values contain no
secret bytes.

Before composition, the caller asks provider admission only for
observe/create/replace/delete, the exact zone,
record types `A`, optional `AAAA`, `MX`, `SRV`, `TXT`, production or OTE as
appropriate, and a fixed bounded timeout. Exact candidate `1.0.0-alpha.2`
admits this request for disposable composition; production remains unavailable
until retained cross-forge publication evidence changes the compiled pin.

## Verification

Tests must cover deterministic output, exact routes/URLs/records/owners,
hostile hostnames, escaping attempts, public/private address rules, duplicate
records, secret-path misuse, owner/listener overlap, candidate versus
production distribution, and fixed error redaction.

Run Caddy `adapt` and `validate` against the generated Caddyfile. Import the
generated Authentik blueprint twice in disposable Authentik 2026.5.2 with
mounted files. Exercise the pinned DNS artifact through compiled admission and
process conformance. Product acceptance must then prove browser OIDC, complete
SCIM lifecycle, durable role grant/revoke, session revocation, and Authentik
outage independence. Unit rendering alone cannot close this child.
