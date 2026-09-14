# Changelog

## Unreleased

### 2026-09-14 — Complete the Authentik runtime adapter slice

Add a digest-pinned Authentik 2026.5 server/worker adapter with direct
loopback PostgreSQL composition, file-referenced secrets, six role-scoped
loopback listeners, exact hardened-container inspection, durable transaction
state, explicit replacement operations, health checks, crash observation, and
persistent-state-preserving reverse rollback. This completes the three private
platform mechanisms but adds no Docker grant, tenant/outpost management,
controller `apply`, live deployment, release, or tag.

### 2026-09-14 — Implement the PostgreSQL runtime adapter slice

Define a component-scoped, digest-pinned PostgreSQL 17 container contract and
add durable runtime replacement, exact observation/readiness, and
data-preserving reverse rollback mechanisms. Docker authority remains outside
the controller CLI; image pulling, secret creation/rotation, SQL migrations,
backups, data deletion, Authentik, product deployment, and `apply` remain out
of scope.

### 2026-09-14 12:40 CDT — Implement the bounded Caddy adapter slice

Split the broad platform-adapter workstream and implement the Caddy child as a
private package. It binds an absolute Caddy executable to its approved artifact
digest, accepts one secret-free complete Caddyfile, validates it with fixed
Caddy commands, stages the prior state durably, replaces the persistent file
atomically, activates through a literal-loopback Admin API, observes exact
runtime digests, reconciles interrupted staging, and rolls back in reverse
order.

This adds no CLI invocation, caller-supplied command or argument path, shell,
systemd mutation,
secret expansion, live deployment, Authentik adapter, PostgreSQL adapter,
release, or tag. Completing this child does not complete the parent platform
workstream or unblock `apply`.

The full gate also exposed that Tailwind's automatic repository scan let
unrelated Go identifiers perturb the website CSS hash. The stylesheet now
disables automatic detection and retains its explicit templ-only source, so
controller code cannot change the public asset by accident.

### 2026-09-14 09:27 CDT — Point the source link at the organization

Change the landing-page GitHub destination from the single repository to the
`gotthboard` organization. The exact-link test also rejects regression to the
repository-specific destination.

### 2026-09-14 09:00 CDT — Link the landing page to its public source

Replace the final internal call-to-action with one ordinary link to the
canonical public GitHub repository. The handler test fixes the exact URL and
requires it to appear once. The server still performs no outbound request and
the complete page remains usable without JavaScript.

### 2026-09-14 07:53 CDT — Build the gotthstack.com public website

Commit: current commit; hash assigned by Git after commit

Affected files:

- `cmd/gotthstack-web/**`
- `internal/site/**`
- `assets/**`
- generated web assets
- project build and dependency manifests
- product, architecture, implementation, workflow, traceability, and
  verification records

Explanation:

Add a deliberately separate public website built with Go, templ, Tailwind CSS,
and HTMX. The site explains both the four-tool web stack and the current
non-mutating controller boundary, provides a progressively enhanced principles
explorer, and embeds every runtime asset. It does not import the controller or
journal and cannot acquire deployment authority.

Verification:

- Pinned double-generation and exact CSS content-address checks.
- Focused and full-repository vet, race, coverage, and build gates with Go
  1.26.6.
- Real HTMX click/focus/history check plus wide, narrow, and JavaScript-disabled
  Chromium checks.
- Strict route, browser-security, import/side-effect, dependency-audit, and
  benchmark evidence in `workflow/features/public-site/evidence/`.

Risks / non-goals:

- No deployment machinery, DNS API, runtime Caddy configuration, account flow,
  analytics, cookie, contact persistence, administrator UI, controller
  mutation, tag, or release.

### 2026-09-14 02:17 CDT — Implement the approval and recovery journal

Add the Linux-local `pkg/journal` implementation: private installation
identity, nonblocking lifetime lock, canonical checksum-framed hash-chain log,
durable head replacement, exact approval projection, operation and step
idempotency, strict phase transitions, restart reconstruction, read-only retry,
mutation reconciliation, rollback compensation, fixed errors, and deep-copy
public results.

The hostile suite covers filesystem object and permission refusal, subprocess
locking, every durable append checkpoint, torn tails, stale and corrupt heads,
checksum/chain/sequence/schema/identity/state corruption, frame/log bounds,
expiry, duplicate/conflicting requests, phase order, failure points,
cancellation, unknown mutation outcomes, rollback, and terminal states.

Cold hardening raised the frame bound from 1 MiB to 4 MiB after a largest-valid
plan proof showed that the complete bounded approval projection exceeds the
smaller limit. It also validates opened root/lock/log descriptors and their
effective-UID ownership, removes a create-time corrupt-directory leak, permits
operator rollback without a
fabricated failure, and forbids ordinary terminal failure after mutation may
have left external state live. A subsequent authority review made approvals
single-operation capabilities so a fresh operation ID cannot replay one human
approval. The same review serializes unfinished operations per installation so
separately approved plans cannot race external state, and adds deterministic
active-operation discovery plus approval lookup so restart does not depend on
a second authority database or allocate the entire history.

This remains a non-executing `1.0.0-alpha.N` workstream. It adds no adapter,
apply command, network client, shell, secret retrieval, host mutation,
deployment, tag, or release.

Exact journal code source
`4a140590ed90472caad93d9d526fd4bbc1d89eb2` passed ZFS-backed race
verification, 100 race repetitions, 20 shuffled runs, two fuzz targets,
hostile replay and durability-checkpoint tests, and two clean cold reviews.
`pkg/journal` reached 84.3% statement coverage; the explicit remaining gap is
limited to direct OS-error scaffolding, impossible fixed-struct marshal
failures, and defensive branches public validation makes unreachable.

### 2026-09-14 01:28 CDT — Define the approval and recovery journal contract

Contract commit: `e6b6e8f`

Affected files:

- `README.md`
- `docs/prd.md`
- `docs/architecture.md`
- `docs/implementation-spec.md`
- `docs/feature-plan.md`
- `docs/verification.md`
- `docs/traceability.md`
- `workflow.toml`
- `workflow/features/v1-operation-journal/README.md`
- `workflow/artifacts/global-coverage-map.md`
- `workflow.events.jsonl`

Explanation:

Replace the vague journal placeholder with stable `STACK-JRN-001` through
`STACK-JRN-012` requirements, the Linux file/directory synchronization and
locking contract, exact approval and secret-revision binding, bounded framed
storage, duplicate-request idempotency, replay, corruption refusal,
cancellation limits, rollback metadata, and explicit unknown mutation outcome.
Clarify that historical `v0` through `v6` workflow IDs are internal labels;
the product remains on the `1.0.0-alpha.N` release line.

Verification:

- `git diff --check -- .`
- `go test -mod=readonly ./...`
- `go vet -mod=readonly ./...`
- direct review of Go 1.26.6 `os.File.Sync`/`os.Rename` documentation and Linux
  7.1 `fsync(2)`, `rename(2)`, and `flock(2)` contracts

Risks / non-goals:

- This commit defines the contract only. It adds no journal implementation,
  adapter, apply command, secret handling, network call, deployment, tag, or
  release.

### 2026-09-14 — Correct the GOTTH Mail runtime topology

Remove the false Mailu runtime dependency from the README, PRD, architecture,
feature plan, workflow, and example manifest. The stack now models the actual
GOTTH Mail control plane with front/proxy, Postfix, Dovecot, Rspamd, and
PostgreSQL components. Mailu remains only a GOTTH Mail-owned historical
reference/import compatibility concern, never a `gotth-stack` component.

This correction changes no planner code, runtime, credential, DNS record, live
host, tag, release, or GitHub mirror. Exact correction source
`c9e6545ca15bb347ab4b084a3a7797c12f389a30` passed the development-host
verification recorded with Forgejo PR #2.

### 2026-09-13 — Create the non-mutating stack plan kernel

Create the MIT-licensed `gotth-stack` repository, canonical product and safety
contracts, strict workflow decomposition, secret-free manifest model,
deterministic dependency planner, digest-bound output, and read-only CLI.

This slice does not deploy or inspect a host, load adapters, read secret values,
or call Docker, systemd, Caddy, Authentik, PostgreSQL, mail daemons, DNS, GOTTH
Mail, or GOTTH Board. Exact source `baec0ebc089ff09ec2975ec23130aa9f35f60852`
passed the development-host gates recorded in the workflow evidence. Forgejo
PR #1 records final review and exact fast-forward admission.
