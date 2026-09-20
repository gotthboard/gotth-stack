# Changelog

## 2026-09-20 — Decompose and specify GOTTH Mail deployment

- Activated the Mail stack workstream and froze the missing production
  artifact, topology, listener, certificate, secret, storage, lifecycle,
  backup, restore, recovery, monitoring, and verification contracts.
- Split implementation into typed role adapters, journal-bound controller
  execution, identity/provider composition, and complete disposable
  acceptance. No apply path or live mutation was added.
- Explicitly rejected the development reference Compose stack as production
  input; Mailu, fixture secrets, runtime package installation, Roundcube, and
  Telegram fixtures are not production components.
- Full repository verification and two cold contract reviews passed; the
  contract child is complete and the typed runtime-adapter child is active.

## Unreleased

### 2026-09-20 14:56 CDT — Prove real Rspamd replacement and preserve mutable mount identity

Commits: `7183ec3`, `370aabd`

Affected files:

- `Makefile`;
- `internal/adapters/mailruntime/bindings_linux.go`;
- `internal/adapters/mailruntime/integration_test.go`;
- `internal/adapters/mailruntime/process_test.go`;
- `internal/adapters/mailruntime/runtime_test.go`;
- `docs/CHANGELOG.md`.

Explanation:

Added an opt-in disposable integration proof that consumes Mail's exact
canonical release manifest and configuration archive, installs the
digest-bound production Rspamd image through the typed adapter, replaces its
controller-secret revision, closes and reopens the adapter after mutation,
then rolls back to the previous running container while retaining durable
Bayes state.

The repository gate now has a dedicated Mail target that enforces the fixed
process boundary, rejects shell/generic apply/image-pull/destructive-volume
paths in production source, and runs focused vet, race, and coverage checks on
every full verification.

The proof exposed a false conflict in mount identity. Directory identity
included `st_size`, but a writable persistent directory legitimately changes
size when its daemon creates state. Directory bindings now retain device,
inode, owner, group, and mode while ignoring content-dependent size. Regular
files still retain size in their identity, and directory replacement remains
rejected.

Verification:

- focused Mail runtime serial, race, and vet checks pass;
- the mutable-directory regression permits content creation after staging;
- the existing inode-replacement regression still rejects a substituted
  directory;
- the real production Rspamd install/replacement/reopen/rollback proof passes
  on the development host in 47.8 seconds and preserves its SQLite Bayes file;
- Mail's release assembler produces identical artifacts twice, and Stack
  accepts the exact manifest plus all eight archive members.
- two cold reviews and the full repository gate admit the runtime-adapter
  child; the journal-bound controller is now the active workstream.

### 2026-09-20 14:25 CDT — Admit Dovecot's bounded login chroot

Commit: `aa90b7c`

Affected files:

- `internal/adapters/mailruntime/open_linux.go`;
- `internal/adapters/mailruntime/process_test.go`;
- `docs/CHANGELOG.md`.

Explanation:

The exact production Dovecot 2.4 login service uses its documented internal
chroot and failed under the adapter's prior capability set. Added `SYS_CHROOT`
only to the fixed Dovecot role. Postfix and all non-root roles retain their
existing narrower sets, and effective-container inspection still rejects any
capability not present in the role definition.

Verification:

- focused Mail runtime serial, race, and vet checks pass;
- role argument coverage proves that only Dovecot receives `SYS_CHROOT`;
- the combined production smoke passes Dovecot startup, LMTP delivery, and
  authenticated IMAPS readback under the exact adapter capability set;
- `git diff --check` passes.

### 2026-09-20 14:00 CDT — Make the private runtime tmpfs usable by fixed Mail UIDs

Commit: `292a5d9`

Affected files:

- `internal/adapters/mailruntime/process.go`;
- `internal/adapters/mailruntime/inspect.go`;
- Mail runtime process and lifecycle tests;
- `docs/CHANGELOG.md`.

Explanation:

The combined production-role proof found that Docker created the `/run` tmpfs
as root-owned while the control, front, and Rspamd roles run as fixed UID/GID
1000 without discretionary-access capabilities. Rspamd therefore could not
create its required private control-socket directory. The adapter now creates
the bounded `/run` tmpfs with explicit UID/GID 1000 and mode 0700 and rejects
containers that do not preserve that exact ownership contract. Root Postfix
and Dovecot retain only their already documented capabilities and can operate
inside the same private runtime tree.

Verification:

- focused Mail runtime serial, race, and vet checks pass;
- argument and effective-container tests cover the exact tmpfs ownership;
- the production smoke proceeds past Rspamd startup under the adapter's
  read-only-root and dropped-capability boundary;
- `git diff --check` passes.

### 2026-09-20 13:45 CDT — Wait for bounded Mail role readiness

Commit: `800ecf9`

Affected files:

- `internal/adapters/mailruntime/actions.go`;
- `internal/adapters/mailruntime/model.go`;
- `internal/adapters/mailruntime/runtime_test.go`;
- `docs/CHANGELOG.md`.

Explanation:

Replaced the one-shot container health check with a bounded readiness loop.
The production Rspamd role performs a first-start ruleset compilation that can
take roughly thirty seconds on the deployment-class development host; treating
the first failed probe as terminal made a healthy immutable container
undeployable. The adapter now retries at a fixed interval only within its
existing operation context, whose default bound is raised to sixty seconds and
whose caller-supplied one-minute maximum is unchanged. A container that never
becomes ready still fails closed as `ErrContainer` and remains subject to the
existing rollback state machine.

Verification:

- a regression test proves two transient failures followed by readiness;
- the existing failure and rollback tests retain the public error contract;
- focused serial, race, and vet checks pass for the Mail runtime adapter;
- `git diff --check` passes.

### 2026-09-20 13:34 CDT — Bind Mail runtime adapters to production secrets and ports

Commit: `301cb77`

Affected files:

- `internal/adapters/mailruntime/model.go`;
- `internal/adapters/mailruntime/open_linux.go`;
- Mail runtime adapter unit, process, and lifecycle tests;
- `docs/CHANGELOG.md`.

Explanation:

Aligned the typed runtime adapters with the production GOTTH Mail image
contract. The control plane now receives the complete database URL, OIDC
client secret, front-auth credential, extension master key, and distinct
Postfix helper/release credentials through immutable read-only files. The
front receives its matching auth credential, and Postfix receives its two
independent queue credentials. The stale database-password target was
corrected to the file interface the product actually reads. Postfix and
Dovecot receive the explicit `NET_BIND_SERVICE` capability required by their
native private ports; all other dropped-capability, read-only-root,
no-new-privileges, revision-digest, and rollback constraints remain intact.

Verification:

- focused serial and race tests pass for `internal/adapters/mailruntime`;
- focused vet passes;
- immutable multi-secret revision tests and replacement rollback pass;
- `git diff --check` passes.

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
