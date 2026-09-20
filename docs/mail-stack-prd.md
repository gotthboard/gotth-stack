# GOTTH Mail stack requirements

## Problem

GOTTH Mail has a verified product implementation and a development reference
stack. It does not yet have a deployable production artifact or a controller
path that can install, upgrade, verify, back up, restore, and roll it back.
The reference Compose topology is not admissible production input: it contains
fixture data, development secrets, runtime package installation, broad port
publication, and a Telegram fixture.

## Boundary

GOTTH Stack deploys exact prebuilt artifacts. It never builds from a branch,
installs packages at container start, invents secrets, edits product data, or
turns the reference stack into production by changing an environment name.
GOTTH Mail owns product schema, generated daemon configuration, mail policy,
identity binding, backup content, and application migrations. Stack owns exact
deployment authority, process ordering, listener ownership, health admission,
backup-before-change, rollback coordination, and recovery state.

Mailu is not a runtime dependency. Telegram is neither required nor installed
by this feature. The existing notification behavior remains compatible, while
the admitted webhook extension may be selected independently.

## Requirements

- `STACK-MAIL-001`: Admit exactly one release manifest whose bytes, annotated
  tag, source commit, and artifact checksums have exact canonical Forgejo and
  public GitHub parity. The manifest binds the GOTTH Mail, front/proxy,
  Postfix, Dovecot, and Rspamd image repository digests, product version,
  configuration digest, schema range, and required extension artifacts.
  Floating tags, branch builds, mirror disagreement, and checksum-only source
  labels fail closed. Artifact signatures may strengthen this boundary later;
  no nonexistent signing authority is claimed now.
- `STACK-MAIL-002`: Use five role-scoped adapter identities. Shared internal
  Docker mechanics may be reused, but no role receives another role's mounts,
  ports, secrets, health command, or mutation capability.
- `STACK-MAIL-003`: Assign public listeners without overlap. Caddy owns TCP 80
  and 443 for the administrator, webmail, OIDC callback, SCIM API, MTA-STS,
  and HTTP ACME challenge. Mail front/proxy owns TCP 25, 465, 587, 143, and
  993. PostgreSQL, GOTTH Mail internal APIs, Postfix, Dovecot, and every Rspamd
  worker remain private or literal-loopback only. Port 465 and 993 require
  implicit TLS; 587 and 143 require STARTTLS before authentication; cleartext
  authentication is disabled; port 25 follows the configured MTA TLS policy
  and never accepts mailbox-submission credentials. POP3 is not exposed.
- `STACK-MAIL-004`: Bind one certificate lifecycle owner per hostname and
  listener. Front/proxy receives only the exact mail certificate/key revision;
  Caddy owns web certificates. ACME and DNS provider grants cannot cross these
  boundaries.
- `STACK-MAIL-005`: Bind secrets only through existing owner-only regular
  files with recorded revision digests. Secret bytes never enter manifests,
  plans, journal records, Docker arguments, container inspection, result
  values, errors, or evidence.
- `STACK-MAIL-006`: Use a private internal network, derived container and
  network names, fixed local Docker engine, locally present digest-pinned
  images, read-only roots where the daemon permits, no-new-privileges, minimal
  capabilities, bounded tmpfs, explicit non-root runtime users where supported,
  and no Docker socket in any application container.
- `STACK-MAIL-007`: Mount separate persistent roots for PostgreSQL, mailbox
  data, queue data, DKIM material, product backup artifacts, extension state,
  and controller transactions. Every root is descriptor-validated, privately
  owned, non-symlinked, and included in the backup/restore contract appropriate
  to its consistency model.
- `STACK-MAIL-008`: Render complete production configuration before mutation.
  Configuration contains no reference fixtures, development secrets, runtime
  package installation, raw environment secrets, public Rspamd listener, or
  implicit self-signed TLS fallback.
- `STACK-MAIL-009`: Preflight the complete topology, image metadata, effective
  container specifications, listener ownership, certificate revisions,
  database identity, product version/schema compatibility, and extension
  artifact pins without changing runtime state.
- `STACK-MAIL-010`: Expose each stop, rename, create, start, configuration
  install, migration, and route activation as a separate journalable action.
  Durable intent precedes every effect. Unknown mutation outcomes require
  observation and reconciliation; they are never blindly retried.
- `STACK-MAIL-011`: Admit health only after exact effective-state inspection
  and bounded active checks for product health/version, Postfix SMTP, Dovecot
  IMAP, Rspamd scan/controller, front listener/TLS, extension health, and
  PostgreSQL identity. A green container state alone is insufficient.
- `STACK-MAIL-012`: Prove authenticated OIDC login and callback, SCIM create,
  update, disable, restore, and delete, durable role projection, application
  session revocation, and Authentik-outage independence of existing mail
  delivery and daemon lookup.
- `STACK-MAIL-013`: Capture a consistent backup before upgrade: PostgreSQL
  logical/product state, mailbox bytes, queues, DKIM material, generated
  configuration, extension inventory/secrets by revision, image/artifact
  digests, and deployment policy. Verification restores into an isolated empty
  topology and runs identity plus mail-flow checks before the backup is usable
  for rollback authority. Master keys and provider credentials are not copied
  into the ordinary backup; the manifest binds separately custodied encrypted
  recovery material by revision and digest, and restore preflight proves those
  revisions are available without disclosing them.
- `STACK-MAIL-014`: Upgrade only within an explicit compatibility edge. Schema
  migration and runtime replacement are separately journaled. Rollback may
  restore prior runtime against forward-compatible data or perform an explicit
  verified restore; it may never silently rewind data or claim that container
  rollback reversed a migration.
- `STACK-MAIL-015`: Disposable acceptance proves two hosted domains, inbound
  and outbound SMTP, submission, IMAP, webmail, DKIM, same-domain-only policy,
  aliases/forwarding, automatic mail, queue retry/replay, extension alerts,
  restart, upgrade, backup, isolated restore, rollback, and recovery from an
  interrupted mutation. Every outbound user/system message exercises the
  exact-sender OpenPGP rule with no unsigned fallback. The administrator and
  webmail surfaces pass the declared keyboard, narrow-view, no-JavaScript,
  theme, CSP, hostile-MIME, and attachment gates.
- `STACK-MAIL-016`: Production apply additionally requires exact DNS and
  reverse-DNS readiness, a public hostname/domain decision, backup destination,
  monitoring destination, operator approval, and a clean disposable proof of
  the same candidate. Missing provider authority remains blocked rather than
  falling back to manual hidden mutation. Forward DNS is changed only through
  an admitted provider extension; reverse DNS requires a separately recorded
  host-provider grant because ordinary zone authority cannot create PTR state.
  Monitoring must alert on health, queue, certificate, extension, backup, and
  policy-hold failures through a tested destination without message or secret
  disclosure.

## Non-goals

- Mailu deployment or import behavior inside Stack;
- a generic Docker/Compose executor or arbitrary shell;
- provider-specific DNS code in this repository;
- POP3, multi-host clustering, Kubernetes, or zero-downtime major migration;
- secret creation, display, or custody in manifests or journal records;
- treating the reference Compose stack as a production artifact.
