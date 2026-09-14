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
  files, identity mismatch, unsupported storage format, and concurrent writers.
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
  or mutation digest for different content fails closed.
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
