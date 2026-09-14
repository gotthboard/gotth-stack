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

## V0 requirements

- `STACK-V0-001`: Parse at most one MiB of strict JSON with no unknown fields,
  duplicate object names, trailing values, or nesting deeper than 32 levels.
- `STACK-V0-002`: Model one named stack containing 1–64 components. Each
  component declares a bounded ID, adapter identity, immutable artifact source
  plus SHA-256 digest, configuration SHA-256 digest, dependencies, requested
  capabilities, and named secret slots. Secret values have no representation.
- `STACK-V0-003`: Reject missing, duplicate, self, and cyclic dependencies;
  reject duplicate capabilities and secret slots.
- `STACK-V0-004`: Produce a deterministic dependency-ordered plan and SHA-256
  identities independent of manifest list ordering.
- `STACK-V0-005`: Rejection errors never interpolate manifest values. Plans
  display accepted non-secret identifiers and digests, while secret values have
  no schema representation.
- `STACK-V0-006`: Expose only `validate` and `plan` CLI operations. V0 performs
  no network, process, filesystem mutation, host inspection, or apply action.

## Planned product requirements

- Durable, fsync-backed operation journal and recovery after interruption or
  unknown outcomes.
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
- No live deployment in V0.

## V0 acceptance

- Strict parsing, validation, graph, determinism, redaction, CLI, fuzz, race,
  and clean-clone gates pass.
- Independent consumer code can import `pkg/stack`.
- Source and review prove the complete absence of an apply or arbitrary-command
  path.
