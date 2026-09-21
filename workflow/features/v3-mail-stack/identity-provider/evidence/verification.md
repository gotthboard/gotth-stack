# Mail identity/provider composition verification — 2026-09-21

## Candidate

The admitted local implementation ends at
`79a1547d812cb3739dd24b0d6bdff262c92b9612` with tree
`ce5737b7ebb289ec9641d44283ecad1414f51bac`.

The exact retained provider candidate is:

```text
gotth-extension-godaddy-dns-1.0.0-alpha.2-linux-amd64.tar.gz
SHA-256 88ce5fb3c2bd2dc1f66707a8657886055632a31e0a4e7e1cec8e459e0742435f
source c1f3525753da982a3ba84a5f443ae8e35dadebc8
evidence head 8529e8d13dcd7634c6810b1b937fb652564283a4
```

The Authentik renderer dependency is exact commit
`7f7f86e21fbb7ca85e3a0ba78e381509326d6d03`. GOTTH Mail's separately
admitted role-binding operator is exact commit
`c3d40453b4010026263ea1749b82812a52d03596`.

## Proven boundary

- composition is pure, closed, deterministic, and tamper-evident;
- caller-selected secret paths, Caddy fragments, records, commands, and
  publication booleans do not exist;
- generated Caddy servers admit only HTTP/1.1 and HTTP/2 on TCP 80/443;
- web, identity, MTA-STS, and mail certificate/listener ownership is disjoint;
- fixed OIDC and SCIM file references agree with the admitted runtime/import
  boundaries and rendered output contains no credential values;
- provider admission, configuration, instance, capability, grant, session,
  source, and distribution evidence are exact and seal-bound;
- record data and joined owner names fit the exact provider and DNS limits;
- production addresses, zones, report URIs, and derived hostnames fail closed;
- candidate publication cannot authorize production; and
- PTR authority plus Authentik blueprint application remain named blockers
  even after hypothetical provider publication.

## Gates

```text
go vet ./internal/composition/mailidentity ./internal/extensions/godaddydns
PASS

GOTTH_GODADDY_ARTIFACT=<exact archive> \
  go test -race -count=5 -shuffle=on \
  ./internal/composition/mailidentity ./internal/extensions/godaddydns
PASS

GOTTH_GODADDY_ARTIFACT=<exact archive> go test -coverprofile=<file> \
  ./internal/composition/mailidentity ./internal/extensions/godaddydns
PASS; mailidentity 95.6%, godaddydns 83.0%, combined 90.6%

make provider-conformance \
  GOTTH_GODADDY_PROVIDER_REPO=<exact source worktree>
PASS

make mail-identity-caddy-conformance GOTTH_GODADDY_ARTIFACT=<exact archive>
PASS; exactly TCP 80/443, no UDP/443

DOCKER_SUDO=1 make mail-identity-authentik-conformance \
  GOTTH_GODADDY_ARTIFACT=<exact archive>
PASS; Authentik 2026.5.2 missing-secret rejection and double import

git diff --check
PASS
```

```text
PATH=<checksum-verified-node-26.7.0>:$PATH make verify
PASS on evidence head 98c9524bd3928b0c825490fa93d1db089850e001

fresh local clone at the same evidence head:
  make verify
  make provider-conformance
  make mail-identity-caddy-conformance
PASS; tracked tree clean
```

The complete repository gate used Go 1.26.6, Node 26.7.0, and npm 12.0.2.

## Review

Eight rejecting passes found and closed Caddy protocol leakage, mutable result
authorization, false readiness, provider-bound violations, special-use
address/domain gaps, secret-path disagreement, derived-name overflow,
mapped/scoped address escapes, invalid report URI/mailbox forms, and joined
DNS owner overflow. Fresh pass 9 returned CLEAN. Independent merge-authority
pass 10 returned CLEAN/ACCEPT on the same tree. See
[`../review/judge-loop.md`](../review/judge-loop.md).

## Honest limits

This is desired-state composition and disposable renderer evidence, not a live
mail deployment. Provider publication, PTR authority, an admitted Authentik
blueprint-application mechanism, public hostname and deployment authority,
browser OIDC, complete SCIM disable/restore, durable role grant/revoke,
session revocation, Authentik-outage independence, mail flow, backup/restore,
rollback, monitoring, accessibility, and owner acceptance remain unproved.
The feature therefore remains `in_progress` and production readiness remains
false.
