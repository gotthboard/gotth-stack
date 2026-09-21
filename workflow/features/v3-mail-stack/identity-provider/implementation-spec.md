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
func (Result) Verified() bool
```

There is no options map, arbitrary site, arbitrary record, arbitrary Caddy
directive, environment-variable passthrough, apply method, or secret-value
field.

`godaddydns.Result` carries an unexported verification marker set only by
`VerifyAndAdmit`. Composition rejects zero-value or caller-constructed results,
parses and compares the admitted canonical provider configuration, and binds
the admission, manifest, grant, and session digests. A private seal covers
every result field and digest; consumers must call `Verified()` before using
output. Provider publication alone cannot clear the separately named PTR and
Authentik-blueprint-application blockers.

## Validation

Canonical lowercase ASCII/punycode hostnames must lie inside the exact zone
and be mutually distinct. Production requires public hostnames and at least one
canonical public IP. Its zone must have a registrable domain beneath an
ICANN-managed suffix in the pinned Public Suffix List snapshot and must not be
under `.arpa` or any non-ARPA name in the IANA Special-Use Domain Names
registry snapshot dated 2026-05-22. Disposable mode admits `.test` plus
documentation IPs.
Production address validation rejects every non-globally-reachable range in
the IANA IPv4/IPv6 special-purpose registry snapshot verified 2026-09-20 and
rejects IPv4-mapped and interface-scoped IPv6 addresses explicitly.
Upstreams are canonical `host:port` values whose hosts are literal loopback or
private IP addresses. Secret references are not inputs. OIDC is fixed to the
mail runtime adapter target `/run/secrets/oidc-client-secret`; the distinct
one-shot SCIM import reference is `/run/secrets/scim-client-token`.

DKIM is a canonical base64 exact TXT value bounded to the provider's 512-byte
record-data limit. Report destinations are exact `mailto:`
addresses inside the zone with mailboxes bounded to 254 bytes. Every generated
record's data is independently bounded to the provider's 512-byte limit. TTL
is fixed at 600 seconds. Duplicate logical records reject before rendering.

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
records, fixed secret-path output, owner/listener overlap, candidate versus
production distribution, and fixed error redaction.

Run Caddy `adapt` and `validate` against the generated Caddyfile, then start it
inside an isolated network namespace and prove exactly TCP 80/443 with no
UDP/443 listener. Import the generated Authentik blueprint twice in disposable Authentik 2026.5.2 with
mounted files. Exercise the pinned DNS artifact through compiled admission and
process conformance. Product acceptance must then prove browser OIDC, complete
SCIM lifecycle, durable role grant/revoke, session revocation, and Authentik
outage independence. Unit rendering alone cannot close this child.
