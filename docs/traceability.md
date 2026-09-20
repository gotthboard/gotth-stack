# Requirements traceability

GOTTH Stack uses DO-178C-inspired traceability. This is not a certification
claim. The table is an admission index; source and raw verification evidence
remain authoritative.

## GOTTH Mail stack

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-MAIL-001` | release identity in `mail-stack-prd.md` | product release manifest parser | strict manifest/archive/version/digest tests | runtime slice verified at `370aabd` |
| `STACK-MAIL-002` | role authority in `mail-stack-architecture.md` | five typed `mailruntime` role adapters | role capability and cross-role denial matrix | runtime slice verified at `370aabd` |
| `STACK-MAIL-003/004` | fixed topology and certificate ownership | front plus Caddy composition | port collision, listener, TLS, and ACME ownership tests | contracted |
| `STACK-MAIL-005` | file-only secret flow | controller secret revision resolver | owner/mode/symlink/descriptor/redaction tests | adapter boundary verified; controller pending |
| `STACK-MAIL-006/007` | runtime and storage boundary | typed runtime adapters | exact effective inspection and persistent-root identity tests | runtime slice verified at `370aabd` |
| `STACK-MAIL-008/009` | configuration and preflight | product artifact plus adapter preflight | native config checks and no-reference-fixture source scan | adapter boundary verified; topology controller pending |
| `STACK-MAIL-010` | lifecycle and journal binding | controller | crash-point, unknown-outcome, retry, reconciliation, and rollback tests | contracted |
| `STACK-MAIL-011` | active health | adapter and controller health gates | real SMTP/IMAP/Rspamd/product/extension/PostgreSQL checks | adapter health verified; controller/topology pending |
| `STACK-MAIL-012` | identity lifecycle | identity/provider child | disposable browser OIDC and SCIM lifecycle/outage tests | contracted |
| `STACK-MAIL-013/014` | backup, restore, upgrade, rollback | acceptance child | consistent capture, isolated restore, migration edge, and reverse tests | contracted |
| `STACK-MAIL-015/016` | disposable and production admission | acceptance evidence | two-domain full lifecycle, monitoring, DNS/rDNS, and exact-candidate proof | contracted |

## Approval and recovery journal

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-JRN-001` | `docs/architecture.md` approval journal; `docs/implementation-spec.md` storage and lock | `pkg/journal/open_linux.go`, `pkg/journal/storage.go` | permissions, ownership, symlink, identity, descriptor, and subprocess-lock tests | verified at `4a140590` |
| `STACK-JRN-002` | approval binding sections | `pkg/journal/approval.go` | largest-valid plan, exact projection, secret revision, expiry, and lookup tests | verified at `4a140590` |
| `STACK-JRN-003` | idempotency sections | `pkg/journal/approval.go`, `pkg/journal/operation.go` | duplicate/conflicting ID, single-operation approval, and one-active-operation tests | verified at `4a140590` |
| `STACK-JRN-004` | append durability sequence | `pkg/journal/storage.go` | injected log/head write, sync, rename, directory-sync, truncate, and close failures | verified on ZFS at `4a140590` |
| `STACK-JRN-005` | replay and corruption model | `pkg/journal/replay.go`, `pkg/journal/state.go` | schema, JSON, checksum, sequence, chain, identity, time, state, frame, and log corruption tests | verified at `4a140590` |
| `STACK-JRN-006` | torn-tail recovery rules | `pkg/journal/replay.go`, `pkg/journal/storage.go` | partial header/payload, stale/ahead/mismatched head, full-frame, and every-checkpoint reopen tests | verified on ZFS at `4a140590` |
| `STACK-JRN-007` | write-before-effect step contract | `pkg/journal/operation.go`, `pkg/journal/state.go` | phase order, mutation idempotency, rollback policy, and durable transition tests | verified at `4a140590` |
| `STACK-JRN-008` | interrupted-step recovery model | `pkg/journal/replay.go`, `pkg/journal/operation.go` | exact read-only retry, mutation reconciliation, restart lookup, and repeated-interruption tests | verified at `4a140590` |
| `STACK-JRN-009` | result/failure/rollback state machine | `pkg/journal/operation.go`, `pkg/journal/state.go` | failure point, failed mutation compensation, operator rollback, recovery-only refusal, and terminal tests | verified at `4a140590` |
| `STACK-JRN-010` | cancellation boundary | `pkg/journal/state.go` | approval, preflight, in-flight, mutation, rollback, and terminal cancellation tests | verified at `4a140590` |
| `STACK-JRN-011` | non-disclosing error contract | all journal files | sentinel-error and hostile-value disclosure tests plus strict parser fuzzing | verified at `4a140590` |
| `STACK-JRN-012` | authority boundary and non-goals | `pkg/journal/**`; CLI intentionally unchanged | production-source scan and CLI regression tests | verified at `4a140590` |

The exact commands, environment, results, coverage limits, and filesystem
claims are recorded in
`workflow/features/v1-operation-journal/evidence/verification.md`.

Tool limitations: Go race and unit tests cannot prove that storage hardware
honors flushes after reporting success. Evidence proves GOTTH Stack issues and
checks the documented Linux/Go synchronization operations in the required
order. Power-loss proof on every supported production filesystem remains a
downstream release gate.

## Caddy adapter

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-CADDY-001` | `docs/caddy-adapter-architecture.md` least privilege | `open_linux.go`, `process.go` | binary digest, path, descriptor, ownership, mode, symlink, hardlink, lock, and loopback tests | verified candidate |
| `STACK-CADDY-002` | secret boundary and bounds | `validate.go`, `preflight.go` | size, UTF-8, NUL, import, environment, digest, and disclosure tests | verified candidate |
| `STACK-CADDY-003` | fixed process boundary | `process.go`, `preflight.go` | exact verb/argument/environment, output bound, cancellation, and failure tests | verified candidate |
| `STACK-CADDY-004` | transaction and recovery model | `transaction.go` | exact duplicate/conflict, canonical metadata, private files, checkpoint, and tamper tests | verified candidate |
| `STACK-CADDY-005` | persistent file replacement | `actions.go` | chown/chmod/write/sync/close/rename/directory-sync injection and umask test | verified candidate |
| `STACK-CADDY-006` | runtime activation | `actions.go`, `runtime.go` | previous/candidate/other state matrix, reload failure, post-query mismatch, and real Caddy test | verified candidate |
| `STACK-CADDY-007` | observation and reconciliation | `actions.go`, `transaction.go` | absent/incomplete/present and previous/candidate/other classification across reopen | verified candidate |
| `STACK-CADDY-008` | reverse-order rollback | `actions.go`, `transaction.go` | stage rollback refusal, file restore, runtime reactivation, exact prior-state and unrelated-route proof | verified candidate |
| `STACK-CADDY-009` | fixed failure boundary | all Caddy adapter files | sentinel, hostile-value, command-output, HTTP, storage, and context tests | verified candidate |
| `STACK-CADDY-010` | admission boundary | private adapter only | development race/repeat/coverage/build, source scan, disposable Caddy, clean-copy, and review | verified candidate |

The adapter contains mutation mechanisms but no authority path. The controller
CLI does not import it, and `apply` remains absent. Exact commands, environment,
results, limits, and remaining gaps are recorded in
`workflow/features/v2-platform-adapters/caddy/evidence/verification.md`.

## PostgreSQL runtime adapter

| Requirement | Design / specification | Implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-PG-001` | `docs/postgresql-adapter-architecture.md` authority boundary | `open_linux.go`, `process.go` | root-owned binary digest, fixed socket, private roots, lock, mode, and source-boundary tests | verified candidate |
| `STACK-PG-002` | canonical specification and derived names | `validate.go`, `preflight.go` | ID, image, UID/GID, port, digest, and name tests | verified candidate |
| `STACK-PG-003` | secret revision without persistence | `open_linux.go`, `preflight.go` | descriptor, owner/group, mode, symlink, size, NUL, digest, and tamper tests | verified candidate |
| `STACK-PG-004` | fixed image and container inspection | `inspect.go`, `process.go` | exact image/runtime shape, existence/inspect split, drift, output, and ambiguity tests | verified candidate |
| `STACK-PG-005` | durable transaction staging | `transaction.go` | canonical file, duplicate/conflict, incomplete/final residue, tamper, reopen, and cleanup-refusal tests | verified candidate |
| `STACK-PG-006` | least-privilege container creation | `process.go`, `inspect.go` | exact argument vector and effective user, root, capability, security, port, bind, tmpfs, label, and environment tests | verified candidate |
| `STACK-PG-007` | one journalable call per mutation | `actions.go` | idempotent successor, impossible predecessor, injected failure, and reopen-after-transition tests | verified candidate |
| `STACK-PG-008` | exact observation and readiness | `actions.go`, `inspect.go` | state classification plus fixed `pg_isready`/`psql` and real PostgreSQL identity proof | verified candidate |
| `STACK-PG-009` | same-major replacement and reverse rollback | `actions.go` | fake runtime and two-digest PostgreSQL 17 replacement, restore, restart, and data-preservation proof | verified candidate |
| `STACK-PG-010` | fixed non-disclosing failures | all PostgreSQL adapter files | sentinel, context, command, parser, storage, conflict, and recovery tests | verified candidate |
| `STACK-PG-011` | admission boundary | private adapter only | development race/repeat/shuffle/coverage/full gate, source scan, disposable Docker proof, and cold review | verified candidate |

The private package has mechanism but no controller authority. It does not
pull images, create or rotate secrets, run SQL, delete data, manage Docker,
or add `apply`. Exact evidence is under
`workflow/features/v2-platform-adapters/postgresql/`.

## Authentik runtime adapter

| Requirement | Design / specification | Implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-AUTH-001` | authority boundary | `open_linux.go`, `process.go` | executable/engine and source-boundary tests | verified |
| `STACK-AUTH-002` | canonical specification | `model.go`, `validate.go` | topology, digest, and drift tests | verified |
| `STACK-AUTH-003` | file URI secret contract | `storage_linux.go` | framing, revision, and effective-environment tests | verified |
| `STACK-AUTH-004` | local 2026.5 image contract | `inspect.go` | metadata and pull-on-miss tests | verified |
| `STACK-AUTH-005` | host network and role listeners | `process.go`, `inspect.go` | effective inspection and real pair proof | verified |
| `STACK-AUTH-006` | hardened fixed create shape | `process.go` | argument and hostile drift tests | verified |
| `STACK-AUTH-007` | persistent directory identity | `storage_linux.go` | descriptor drift and marker survival | verified |
| `STACK-AUTH-008` | coherent preflight and stage | `preflight.go`, `transaction.go` | pair and durability tests | verified |
| `STACK-AUTH-009` | role-scoped mutations | `actions.go` | ordered forward/reverse tests | verified |
| `STACK-AUTH-010` | effective state and health | `inspect.go`, `actions.go` | health failure and real pair proof | verified |
| `STACK-AUTH-011` | four-name recovery and rollback | `actions.go` | mixed-state, reopen, rollback tests | verified |
| `STACK-AUTH-012` | admission boundary | private package | full development-host gates and cold review | verified |

The package has runtime mechanisms but no controller authority, tenant
configuration, outpost deployment, Docker-socket mount, or `apply` path.
Exact evidence is under `workflow/features/v2-platform-adapters/authentik/`.

## Public website

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-SITE-001` | public website boundary, source link, and route table | `internal/site`, `cmd/gotthstack-web` | handler source-link assertion and external-process smoke tests | verified candidate `5a340915` |
| `STACK-SITE-002` | HTMX enhancement boundary | page and principles templates | full-page and fragment parity tests; no-script browser check | verified candidate `5a340915` |
| `STACK-SITE-003` | fixed topic selection | principles handler | allowlist, duplicate, missing, and reflected-input negative tests | verified candidate `5a340915` |
| `STACK-SITE-004` | embedded asset and browser-security contract | embedded static package and middleware | asset bytes, cache, CSP, and security-header tests | verified candidate `5a340915` |
| `STACK-SITE-005` | semantic and responsive page contract | templ markup and Tailwind source | markup assertions plus narrow/wide browser captures | verified candidate `5a340915` |
| `STACK-SITE-006` | health, method, shutdown, and timeout contract | mux and command | route/method tests, race test, and subprocess termination smoke | verified candidate `5a340915` |
