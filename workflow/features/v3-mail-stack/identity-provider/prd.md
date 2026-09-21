# Product requirements: mail identity/provider composition

## Problem

The runtime adapters can replace Caddy, Authentik, and GOTTH Mail roles, but
there is no single typed desired state that proves their public routes,
identity URLs, credential-file references, certificate owners, and DNS grant
agree. Hand-assembled fragments would make listener and authority conflicts
invisible. It would also permit a deployment that authenticates a user but has
no supported way to grant the first administrator role.

## Required result

Given one explicit deployment environment, public zone, web hostname,
Authentik hostname, mail hostname, addresses, private upstreams, secret-file
references, DKIM public material, and admitted provider artifact, Stack shall
produce deterministic, digest-bound desired state containing:

1. a complete Caddyfile for only TCP 80/443, routing the web hostname to GOTTH
   Mail, the identity hostname to Authentik, and the per-domain MTA-STS
   hostname to an exact static policy;
2. an Authentik blueprint from the exact pinned `gotth-authentik` API with a
   file-backed confidential OIDC credential and file-backed SCIM token;
3. typed GOTTH Mail runtime variables for issuer, client ID, credential file,
   strict callback, and external SCIM base URL;
4. a certificate ownership table proving Caddy owns the web, identity, and
   MTA-STS hostnames while mail front alone owns the mail hostname on SMTP and
   IMAP listeners;
5. canonical per-zone A/AAAA, MX, SPF, DKIM, DMARC, MTA-STS, TLS-RPT, and
   submission SRV desired records; and
6. an exact least-privilege DNS extension request and admission digest.

The plan shall report candidate distribution as non-production-ready. It may
be used for disposable acceptance but cannot authorize production DNS until
the independently published extension has exact Forgejo/GitHub parity.

## Identity lifecycle requirements

The resulting URLs are exact:

- issuer: `https://<identity>/application/o/gotth-mail/`;
- callback: `https://<web>/api/v1/oidc/callback`;
- SCIM base: `https://<web>/scim/v2`.

Authentik synchronization is restricted to `gotth-mail-users`. ID-token group
claims never grant product roles. GOTTH Mail must expose a separately reviewed,
preview/confirm operator operation that binds one durable verified identity to
one valid product role. Direct SQL is not an operator interface and cannot
satisfy this requirement.

## Security and userspace

- Secret values have no representation; only distinct canonical
  `/run/secrets/` references are accepted.
- All public hostnames are distinct. A certificate or listener has one owner.
- Caddy cannot proxy SMTP/IMAP and mail front cannot own HTTP.
- Upstreams are literal loopback or admitted private addresses with explicit
  ports; no arbitrary Caddy snippets or commands exist.
- DNS records are canonical, sorted, unique, bounded, and inside one admitted
  zone. PTR remains read-only external readiness because zone authority cannot
  manufacture reverse-DNS authority.
- Production rejects candidate-only provider distribution, documentation
  addresses/domains, missing public hostname decisions, or missing role/DNS
  prerequisites.

## Current blockers

Source inspection on 2026-09-20 established two blockers, both now satisfied by
reviewed candidate evidence:

1. GOTTH Mail's role-binding operator is admitted at evidence head
   `c3d40453b4010026263ea1749b82812a52d03596`.
2. GoDaddy SRV mutation is admitted at evidence head
   `8529e8d13dcd7634c6810b1b937fb652564283a4`; Stack consumes exact source
   `c1f3525753da982a3ba84a5f443ae8e35dadebc8` and candidate
   `1.0.0-alpha.2` for `_submission._tcp` composition.

Implementation remains responsible for revalidating and pinning both exact
candidates; candidate status is not production publication.
