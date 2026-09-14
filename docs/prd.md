# Product requirements

## Problem

GOTTH Mail and GOTTH Board require repeatable deployment with Caddy,
Authentik, PostgreSQL, and their supporting components. GOTTH Mail additionally
requires its own front/proxy, Postfix, Dovecot, and Rspamd runtime. The current
mechanisms are individually useful but no product owns dependency ordering,
cross-component preview, exact approval, health admission, upgrade, backup, or
rollback for the complete stack.

## Product boundary

`gotth-stack` is a dedicated bootstrap and deployment controller. It is not a
normal runtime extension, and no GOTTH product must already be running for the
controller to start. It may call narrow provider extensions after their exact
artifacts, capabilities, transport identities, and grants are admitted.

Shared orchestration owns dependency order, plan identity, confirmation,
journaling, health gates, and rollback coordination. Separate adapters own the
mechanics for Caddy, Authentik, PostgreSQL, the GOTTH Mail control plane,
front/proxy, Postfix, Dovecot, Rspamd, and GOTTH Board. Provider-specific work
such as GoDaddy DNS remains in an independent `gotth-extension-<slug>`
repository.

## Plan-kernel requirements

The historical workflow ID `v0.plan-kernel` predates the release-stage naming
rule. It is an internal workstream, not a product version. GOTTH Stack remains
on the `1.0.0-alpha.N` release line while any required workstream is incomplete.

- `STACK-PLAN-001`: Parse at most one MiB of strict JSON with no unknown fields,
  duplicate object names, trailing values, or nesting deeper than 32 levels.
- `STACK-PLAN-002`: Model one named stack containing 1–64 components. Each
  component declares a bounded ID, adapter identity, immutable artifact source
  plus SHA-256 digest, configuration SHA-256 digest, dependencies, requested
  capabilities, and named secret slots. Secret values have no representation.
- `STACK-PLAN-003`: Reject missing, duplicate, self, and cyclic dependencies;
  reject duplicate capabilities and secret slots.
- `STACK-PLAN-004`: Produce a deterministic dependency-ordered plan and SHA-256
  identities independent of manifest list ordering.
- `STACK-PLAN-005`: Rejection errors never interpolate manifest values. Plans
  display accepted non-secret identifiers and digests, while secret values have
  no schema representation.
- `STACK-PLAN-006`: Expose only `validate` and `plan` CLI operations. The plan
  kernel performs
  no network, process, filesystem mutation, host inspection, or apply action.

## Approval and recovery journal requirements

- `STACK-JRN-001`: Create or reopen exactly one stable installation identity
  in a private local journal directory. Reject symlinks, non-regular journal
  files, foreign ownership, identity mismatch, unsupported storage format, and
  concurrent writers.
- `STACK-JRN-002`: Record an explicit approval before any operation. Approval
  binds the installation ID, actor ID, authenticated-authority evidence digest,
  issue and expiry times, exact manifest and plan digests, ordered component and
  adapter set, artifact and configuration digests, capability grants, and one
  revision digest for every named secret slot. Secret values have no journal
  representation.
- `STACK-JRN-003`: Approval, operation, and step IDs are caller-supplied
  idempotency keys. Mutation idempotency digests are unique within an
  operation. One approval authorizes exactly one operation ID. Repeating the
  exact request returns the existing durable result; reusing an approval, ID,
  or mutation digest for different content fails closed. One installation has
  at most one unfinished operation so separately approved plans cannot race.
- `STACK-JRN-004`: Persist every journal transition as a bounded,
  checksum-framed, sequence-numbered, hash-chained record. A successful append
  means the record and its durable head have passed the required file and
  directory synchronization steps.
- `STACK-JRN-005`: Reconstruct operation state only from validated durable
  records. Reject checksum, sequence, hash-chain, schema, identity, head, size,
  and state-machine corruption without returning a partial trusted view.
- `STACK-JRN-006`: Recover only an incomplete uncommitted tail whose durable
  head still names the preceding valid record. Never truncate or bless a fully
  framed corrupt record, a missing committed prefix, or a head that points past
  the validated log.
- `STACK-JRN-007`: Before a future adapter may perform a mutating step, record
  its operation, phase, component, stable step ID, attempt, idempotency key, and
  either a rollback-reference digest or an explicit recovery-only reason.
- `STACK-JRN-008`: If restart finds a mutating step with no durable result, the
  operation state is `recovery_required`. The journal never authorizes blind
  retry. An interrupted read-only inspection may be explicitly retried as the
  same step, component, and phase with the next attempt number. A reconciled
  mutation result resumes from that durable result; an unresolved mutation may
  instead be terminated as recovery-required.
- `STACK-JRN-009`: Record step success or failure with a non-secret result
  digest or bounded reason code, the exact failure point, and the transition to
  rollback, rolled-back, complete, cancelled, failed, or recovery-required
  state. Every rollback step names the finished mutating step it compensates;
  even a failed mutation result may represent partial external work and must be
  compensated when a rollback reference exists. Recovery-only mutations cannot
  be reported as rolled back. Conflicting duplicate completion fails closed.
- `STACK-JRN-010`: Cancellation is allowed only before mutation begins and
  while no step is in flight. Once mutation may have occurred, resolution must
  be rollback or explicit recovery.
- `STACK-JRN-011`: Journal errors are fixed sentinel errors and never disclose
  actor values, manifest content, paths, secret-slot revisions, rollback
  references, or stored record payloads.
- `STACK-JRN-012`: This workstream adds no `apply` CLI, adapter invocation,
  network access, shell, Docker/systemd call, secret retrieval, or live host
  mutation. It provides durable authority evidence and recovery state only.

## Public website requirements

The public `gotthstack.com` site is an informational application, not the
controller's future administrator UI. It uses the GOTTH web stack without
receiving any deployment authority.

- `STACK-SITE-001`: `GET /` returns one complete server-rendered landing page
  that identifies Go, templ, Tailwind CSS, and HTMX and accurately describes
  the controller's current alpha boundary. Its source call-to-action links to
  the canonical public GitHub organization. `HEAD /` returns the same status and
  headers without a body.
- `STACK-SITE-002`: The page remains readable, navigable, and complete when
  JavaScript is unavailable. HTMX may enhance the principles explorer but may
  not gate content or navigation.
- `STACK-SITE-003`: `GET /principles` accepts only the documented topic
  allowlist. HTMX requests receive the selected fragment; ordinary browser
  requests receive the complete page with the selected topic. Unknown topics
  fail with `404` and no reflected input.
- `STACK-SITE-004`: CSS and HTMX are versioned, embedded assets served from the
  binary. Public assets use immutable caching; HTML is not stored. Every route
  emits a restrictive content security policy and the documented browser
  security headers.
- `STACK-SITE-005`: The page has one main landmark, a skip link, visible focus
  states, reduced-motion behavior, semantic headings, sufficient contrast,
  and responsive layouts at narrow and wide viewports.
- `STACK-SITE-006`: `GET /healthz` returns the fixed plain-text body `ok\n`;
  unsupported methods and unknown routes fail closed. The process uses bounded
  HTTP timeouts and shuts down on `SIGINT` or `SIGTERM`.

The public site sets no cookie, performs no tracking, accepts no form or
credential, makes no outbound request, and imports no controller, journal, or
adapter package.

## Caddy adapter requirements

The Caddy adapter is the first bounded child of the platform-adapter
workstream. It manages one already-existing complete Caddyfile and the one
running Caddy instance that owns it. It does not compose product routes,
install Caddy, edit systemd, retrieve secrets, or expose an `apply` command.

- `STACK-CADDY-001`: Accept only an absolute Caddy binary bound to its approved
  SHA-256 artifact digest, one absolute existing regular Caddyfile, one private
  adapter-state directory, one explicit service working directory, and a
  loopback HTTP Admin API address. Reject
  binary digest mismatch, symlinks, foreign ownership,
  group/world-writable executable or configuration inputs, broad state modes,
  non-loopback administration, and descriptor-level configuration
  substitution. Both previous and candidate adapted configs must retain the
  exact configured Admin API listener so apply cannot strand verification or
  rollback.
- `STACK-CADDY-002`: Bound a complete UTF-8 Caddyfile at one MiB, bind it to the
  approved SHA-256 configuration digest, and reject NUL bytes, Caddyfile
  imports, and environment substitutions. The current contract is explicitly
  secret-free and single-file; raw configuration and command output never
  appear in errors, results, logs, or evidence.
- `STACK-CADDY-003`: Preflight with the fixed Caddy executable and fixed
  `caddy adapt` plus `caddy validate` arguments, no shell, a bounded output
  sink, and a scrubbed private environment. Preflight must not alter the live
  Caddyfile or running Admin API configuration.
- `STACK-CADDY-004`: Before live configuration mutation, durably stage the
  exact previous and candidate Caddyfiles plus their non-secret file/runtime
  digests in a private operation transaction. Exact duplicate staging is
  idempotent; conflicting operation-ID reuse fails closed.
- `STACK-CADDY-005`: Replace the complete Caddyfile by same-directory temporary
  file, file sync, atomic rename, and directory sync while preserving its safe
  permission mode. Install is idempotent only when the target is exactly the
  staged previous or candidate digest; any third state requires recovery.
- `STACK-CADDY-006`: Activate only the staged candidate with fixed
  `caddy reload` arguments against the configured loopback Admin API. Success
  requires the Admin API's canonical running JSON digest to equal the
  preflight-adapted candidate digest; unexpected runtime state fails closed.
- `STACK-CADDY-007`: Inspect staged, file, and runtime state without mutation so
  an interrupted journal step can be reconciled rather than blindly retried.
  Observation classifies exact previous, exact candidate, absent, or other
  state and discloses only enums and SHA-256 digests.
- `STACK-CADDY-008`: Rollback restores the staged previous file atomically,
  then reloads that file. These are separate reverse-order mutation steps.
  Rollback is complete only when both the file digest and running Admin API
  digest equal the recorded preflight state.
- `STACK-CADDY-009`: Every public failure is a fixed sentinel class. Context
  cancellation, size limits, command failure, HTTP failure, filesystem failure,
  mismatch, and recovery-required state are distinguishable without disclosing
  paths, configuration, process output, URLs, or request data.
- `STACK-CADDY-010`: Hostile unit tests and a disposable real-Caddy test prove
  preflight, durable staging, atomic install, activation, idempotency,
  reconciliation, unexpected-state refusal, rollback, and restart behavior.
  This slice adds no controller `apply` command and touches no live Caddy.

## PostgreSQL runtime adapter requirements

The PostgreSQL adapter manages one PostgreSQL 17 container and its runtime
identity. It does not own application schemas, migrations, backups, secret
creation, data deletion, Docker installation, or a controller `apply` command.
Database contents are durable product state and are never treated as
rollback scratch.

- `STACK-PG-001`: Bind one root-owned Docker-compatible client executable to
  its exact SHA-256 digest and the literal local Unix engine endpoint. Accept
  only private adapter, data, and secret roots with fixed ownership and modes.
  The controller process must never receive generic Docker-socket authority;
  production execution belongs behind a separately installed, component-
  scoped adapter boundary.
- `STACK-PG-002`: Accept one canonical PostgreSQL 17 specification containing
  a bounded component ID, digest-pinned image reference, database and bootstrap
  role identifiers, positive container UID/GID, and one loopback host port.
  Derive the container, data-directory, and password-file names from the
  component ID. Bind the canonical specification to the approved
  configuration digest.
- `STACK-PG-003`: Bind the password file to the approved secret-revision
  digest without returning or persisting its bytes. Reject symlinks, hard
  links, unsafe ownership or modes, empty/oversized/NUL-bearing values, and
  descriptor substitution.
- `STACK-PG-004`: Preflight with fixed `docker image inspect` and
  `docker inspect` calls, bounded output, no shell, and a scrubbed environment.
  The candidate image must have the exact requested repository digest,
  `PG_MAJOR=17`, the official entrypoint, and `postgres` command. Existing
  containers are accepted only when they match the adapter's complete managed
  shape and labels.
- `STACK-PG-005`: Before runtime mutation, durably stage canonical candidate
  and optional previous specifications, image identities, engine identity,
  previous running state, secret identity/revision, and data-directory
  identity. Exact duplicate staging is idempotent; conflicting operation-ID
  reuse fails closed.
- `STACK-PG-006`: Create containers only from package-built arguments: exact
  image digest, derived name, nonroot UID/GID, read-only root filesystem, all
  capabilities dropped, no-new-privileges, bounded noexec temporary filesystems,
  one loopback PostgreSQL port, one data bind, and one read-only password bind.
  No caller supplies arguments, environment entries, labels, or mounts.
- `STACK-PG-007`: Expose stop, preserve-previous-name, create, start, verify,
  and rollback operations separately so the journal can record every possible
  external effect before execution. Repetition succeeds only for the exact
  staged state; any third state requires recovery.
- `STACK-PG-008`: Verify exact effective container inspection plus fixed
  in-container `pg_isready` and read-only `psql` identity queries. Successful
  process exit alone is not readiness. Observation reports only enums and
  digests and distinguishes the primary and rollback container names.
- `STACK-PG-009`: Upgrade and rollback may replace only an adapter-managed
  PostgreSQL 17 runtime while preserving the same database, role, UID/GID,
  port, data directory, and secret revision. Rollback restores the previous
  container runtime. For a fresh install it removes the candidate container
  but deliberately preserves initialized data; it never claims data rollback.
- `STACK-PG-010`: Every public failure is a fixed sentinel class. Context,
  limit, executable, image, engine, container, secret, storage, conflict, and
  recovery failures remain distinguishable without disclosing paths, command
  output, configuration, image names, container names, or secret material.
- `STACK-PG-011`: Hostile unit tests and a disposable real PostgreSQL 17 test
  prove fresh create/start/reopen/verify/rollback, same-major replacement and
  reverse rollback, idempotency, interruption observation, unexpected-state
  refusal, data preservation, fixed process arguments, and source boundaries.
  No live container, data directory, Docker policy, or controller authority is
  changed by this feature.

## Downstream product requirements

- Preview/apply bound to exact actor, plan digest, expiry, target installation,
  adapter set, artifacts, configuration digests, capabilities, and secret-slot
  revisions.
- Least-privilege adapters with explicit preflight, apply, verify, and rollback
  contracts; no Docker socket or root access granted generically.
- Native first-run and administrator UI using Go, templ, Tailwind, and HTMX
  with ordinary HTML fallback.
- Separate Mail and Board adapters sharing orchestration but not product policy,
  databases, credentials, or failure domains.
- One GOTTH Mail deployment may serve multiple mail domains, but each domain's
  DNS, DKIM, TLS, policy, aliases, and mailbox namespace remain explicit.
- Mail delivery and daemon lookup remain available when Authentik is
  unavailable; OIDC/SCIM failure must not become SMTP/IMAP failure.
- Backup and restore proof before any upgrade or destructive change is
  presented as safe.

## Non-goals

- No generic remote shell, arbitrary command runner, or embedded scripting.
- No secret values in manifests, plans, logs, evidence, or audit projections.
- No product-specific conditionals in the shared engine.
- No extension that bootstraps its own extension host.
- No global administrator that silently inherits every product authority.
- No Mailu runtime component or adapter. Mailu reference/import compatibility
  remains GOTTH Mail-owned and is not a deployment dependency.
- No live deployment in the plan-kernel or journal workstreams.
- No public-site analytics, cookies, account flow, contact-form persistence,
  controller mutation, or administrator UI.

## Current alpha acceptance

- Strict parsing, validation, graph, determinism, redaction, CLI, fuzz, race,
  and clean-clone gates pass.
- Independent consumer code can import `pkg/stack`.
- Source and review prove the complete absence of an apply or arbitrary-command
  path.
- Approval/recovery tests prove exact binding, duplicate-request idempotency,
  expiry, durable ordering, torn-tail recovery, corruption refusal,
  cross-process writer exclusion, filesystem-failure propagation, restart
  replay, cancellation limits, rollback metadata, and unknown-outcome handling.
- A source scan proves the journal adds no adapter invocation or live mutation
  path. `apply` remains absent until downstream adapter contracts and disposable
  integration proofs are admitted.
