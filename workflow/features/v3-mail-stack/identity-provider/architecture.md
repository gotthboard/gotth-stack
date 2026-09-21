# Architecture: one typed composition boundary

## Mechanism

`internal/composition/mailidentity` is a pure package. It validates one closed
input and renders one tamper-evident result. It performs no filesystem, DNS,
HTTP, Docker, database, or tenant mutation. The caller separately passes each
output to an admitted adapter or provider operation under journal authority,
which must reject the result unless its private composition seal verifies.

The package imports the exact `gotth-authentik` pseudo-version corresponding
to mirrored commit `7f7f86e21fbb7ca85e3a0ba78e381509326d6d03`.
It does not copy Authentik model names, mapping identifiers, or `!File`
behavior into Stack.

## Data flow

```text
exact provider archive + closed provider request
  `-- godaddydns.VerifyAndAdmit
                 |
typed deployment input + opaque verified admission
  |-- validate host/zone/IP/upstream/secret references
  |-- gotth-authentik.RenderDeploymentBlueprint
  |-- render fixed Caddyfile and MTA-STS policy
  |-- derive GOTTH Mail identity variables
  |-- derive certificate/listener ownership
  `-- derive canonical DNS records and exact provider grant
            `-- bind exact admission, grant, session, and configuration digests
```

Every rendered byte slice and canonical typed collection has a SHA-256 digest.
The aggregate composition digest covers a canonical JSON wire containing only
the environment, public identifiers, component digests, ownership table,
records, provider source/artifact pins, admission/grant/session digests, and release-readiness
state. It contains no credential values or host filesystem paths except the
two fixed container-visible secret references. The private seal covers every
exported output, component digest, blocker, and readiness bit; mutation after
composition invalidates `Verified()`.

## Caddy and certificate authority

Caddy binds only TCP 80/443 and explicitly limits protocols to HTTP/1.1 and
HTTP/2; HTTP/3/QUIC and UDP/443 are not admitted. Three distinct HTTPS site
labels exist: web, identity, and `mta-sts.<zone>`. GOTTH Mail and Authentik
upstreams are fixed private endpoints. The MTA-STS site returns the computed
policy only at `/.well-known/mta-sts.txt` and 404 elsewhere.

Mail front owns 25/465/587/143/993 and the certificate for the distinct mail
hostname. Caddy owns certificates for the other three hostnames. The result is
rejected if any hostname or listener overlaps; sharing a hostname across two
certificate owners is not treated as harmless just because ports differ.

## DNS boundary

Records are desired state, not imperative mutations. The DNS extension must
first observe the admitted zone; later journaled reconciliation selects exact
create/replace/delete operations using the provider's snapshot and record
preconditions. This feature does not invent a second DNS client.

Required mutable record types are `A`, optional `AAAA`, `MX`, `SRV`, and
`TXT`. CAA is outside the minimum record set unless an explicit certificate
policy is added later. Candidate `1.0.0-alpha.2` compiles that exact set and is
admissible for disposable composition. Its unavailable public distribution
proof still prevents production readiness.

## Failure behavior

Validation returns fixed sentinel classes without echoing attacker-controlled
input. No partial output is returned. Composition accepts only an opaque
verified result returned by `godaddydns.VerifyAndAdmit`; callers cannot
construct publication authority. Candidate admission produces an honest
`ReleaseReady=false`; production composition rejects it. Published provider
evidence still cannot make the composition production-ready: PTR authority and
an admitted Authentik blueprint-application mechanism remain named blockers.
The fixed OIDC file path matches the mail runtime adapter; the fixed SCIM token
path is used only by the disposable one-shot import proof and does not pretend
that the current Authentik runtime adapter mounts it.
