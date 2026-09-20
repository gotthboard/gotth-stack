# GOTTH Mail stack architecture

## Fixed topology

```text
Internet
  |-- 80/443 ---------- Caddy ---------- GOTTH Mail HTTP/webmail/OIDC/SCIM
  |-- 25/465/587 ------ mail front ----- Postfix ----- Rspamd
  `-- 143/993 --------- mail front ----- Dovecot
                                               |
                         GOTTH Mail control ----+---- PostgreSQL
                                  |
                                  `---- admitted notification extension
```

Caddy and mail front/proxy are separate certificate/listener owners. Caddy
never proxies SMTP or IMAP. Mail front never serves product HTTP. Postfix,
Dovecot, Rspamd, PostgreSQL, the product policy/helper endpoints, and extension
control sockets are not public listeners.

The mail front uses a complete, generated, digest-bound TCP proxy
configuration and the exact mail certificate revision. It forwards only the
five documented protocols to fixed private role endpoints. Client-address
provenance uses the one configured proxy protocol end to end or the deployment
is rejected; source identity is not reconstructed from untrusted headers.
Implicit TLS is mandatory on 465/993. Authentication on 587/143 is impossible
until STARTTLS completes, and port 25 is never a mailbox-submission credential
endpoint. Certificate/key reload and expiry observation are explicit front
adapter actions, not incidental container restarts.
Backend daemons accept proxy-protocol identity only from the private front
endpoint. Direct private-network connections that claim proxy metadata fail;
public client addresses never come from application headers.

## Authority split

The controller owns dependency order and journal transitions. Five typed role
adapters own bounded Docker mechanisms:

1. GOTTH Mail control plane;
2. mail front/proxy;
3. Postfix;
4. Dovecot; and
5. Rspamd.

A private common engine may implement fixed image/container inspection and
single-container transition mechanics. It is not a public generic adapter:
each typed role supplies a compile-time allowlist for image labels, entrypoint,
command, user, capabilities, mounts, ports, health operation, secret slots,
and dependencies. A role cannot pass arbitrary Docker arguments.

PostgreSQL, Authentik, and Caddy remain owned by their already-admitted
platform adapters. Product tenant configuration uses the exact rendered GOTTH
Mail Authentik profile and SCIM provider desired state; it is not smuggled into
the runtime-pair adapter.

## Artifact and configuration flow

The product release produces one canonical manifest containing the product
version, source commit, image digests, configuration bundle digest, migration
range, extension pins, and archive checksums. Stack verifies it before
preflight. Configuration is generated from typed product state into a private
candidate directory and validated by each daemon's native check command.

Secrets are resolved after approval by `(component, slot, revision)` to
already-existing private files. Only file paths are mounted. The resolver
returns descriptor identity and a digest; it never returns secret bytes to the
plan or journal layer.

## Lifecycle

```text
release manifest -> plan -> approval -> preflight -> verified backup
  -> stage each role -> stop/rename/create/start in dependency order
  -> migrate -> activate routes -> active health -> complete
```

Forward replacement starts private dependencies before public ingress. Public
routes/listeners activate last. Rollback removes public admission first, then
reverses role replacements. If data has crossed a non-reversible migration,
runtime rollback is forbidden until the verified-restore plan is explicitly
approved.

Every adapter transaction records only exact digests, derived identities,
state enums, and rollback references. Observation distinguishes absent,
previous, candidate, mixed, other, and incomplete states. Mixed or other state
requires recovery; it never becomes an implicit reinstall.

## Backup and restore

The backup set is one manifest whose members are independently hashed:

- product/PostgreSQL logical artifact and database dump;
- mailbox tree;
- Postfix queue snapshot captured under the documented quiescence boundary;
- DKIM keys and selector metadata;
- generated configuration and deployment policy;
- extension inventory plus encrypted-secret revision references;
- separately custodied encrypted recovery-material revision digests for the
  product/extension master keys and external-provider credentials;
- exact image, binary, and release-manifest digests.

Backup removes public submission/IMAP admission, drains in-flight product
mutations, pauses Postfix queue runners and Dovecot mailbox writers, checkpoints
PostgreSQL, then captures queue and mailbox snapshots before resuming in the
reverse order. A bounded timeout aborts the capture and leaves the prior
runtime active. Filesystem copying without this sequence is not a consistent
backup. Restore preflight proves every separately custodied recovery revision
is present without recording or displaying its bytes.
Restore targets a new empty identity, never an in-place production directory.
The restored topology must pass schema, daemon lookup, OIDC/SCIM, SMTP, IMAP,
DKIM, policy, queue, and extension checks before the backup becomes rollback
authority.

## Failure and monitoring

Required signals are product health/version, database readiness, SMTP ingress
and submission, IMAP login/read, Rspamd scan, queue age/depth, certificate
expiry, backup age/verification, extension health, and policy-hold backlog.
Monitoring receives bounded status and identifiers, never message bodies,
credentials, mailbox contents, or raw daemon output.

The acceptance sender exercises the product's OpenPGP exact-sender path for
user mail, automatic mail, and notifications. Signing/key failure blocks the
message; deployment health cannot turn an unsigned fallback into success.

An Authentik outage blocks new interactive login and provisioning. It does not
stop Postfix delivery, Dovecot verification against already-projected state,
Rspamd, queue processing, or existing product health. The disposable outage
test is a release gate.

Forward DNS and PTR authority are distinct. The DNS extension can mutate only
the admitted zone/record grant. PTR readiness is observed read-only unless an
independent host-provider adapter and exact grant are admitted; otherwise the
production gate remains blocked.
