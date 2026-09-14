# Requirements traceability

GOTTH Stack uses DO-178C-inspired traceability. This is not a certification
claim. The table is an admission index; source and raw verification evidence
remain authoritative.

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

## Public website

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-SITE-001` | public website boundary, source link, and route table | `internal/site`, `cmd/gotthstack-web` | handler source-link assertion and external-process smoke tests | verified candidate `5a340915` |
| `STACK-SITE-002` | HTMX enhancement boundary | page and principles templates | full-page and fragment parity tests; no-script browser check | verified candidate `5a340915` |
| `STACK-SITE-003` | fixed topic selection | principles handler | allowlist, duplicate, missing, and reflected-input negative tests | verified candidate `5a340915` |
| `STACK-SITE-004` | embedded asset and browser-security contract | embedded static package and middleware | asset bytes, cache, CSP, and security-header tests | verified candidate `5a340915` |
| `STACK-SITE-005` | semantic and responsive page contract | templ markup and Tailwind source | markup assertions plus narrow/wide browser captures | verified candidate `5a340915` |
| `STACK-SITE-006` | health, method, shutdown, and timeout contract | mux and command | route/method tests, race test, and subprocess termination smoke | verified candidate `5a340915` |
