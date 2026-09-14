# Requirements traceability

GOTTH Stack uses DO-178C-inspired traceability. This is not a certification
claim. The table is an admission index; source and raw verification evidence
remain authoritative.

## Approval and recovery journal

| Requirement | Design / specification | Planned implementation | Verification contract | Status |
| --- | --- | --- | --- | --- |
| `STACK-JRN-001` | `docs/architecture.md` approval journal; `docs/implementation-spec.md` storage and lock | `pkg/journal/open_linux.go`, `pkg/journal/storage.go` | create/open, permissions, symlink, identity, lock subprocess tests | contract approved; code pending |
| `STACK-JRN-002` | approval binding sections | `pkg/journal/approval.go` | exact plan and secret-revision projection tests | contract approved; code pending |
| `STACK-JRN-003` | idempotency sections | `pkg/journal/approval.go`, `pkg/journal/operation.go` | exact duplicate and conflicting reuse tests | contract approved; code pending |
| `STACK-JRN-004` | append durability sequence | `pkg/journal/storage.go` | injected append/sync/head/rename/directory-sync failures | contract approved; code pending |
| `STACK-JRN-005` | replay and corruption model | `pkg/journal/replay.go` | schema/checksum/sequence/chain/identity/state corruption tests | contract approved; code pending |
| `STACK-JRN-006` | torn-tail recovery rules | `pkg/journal/replay.go`, `pkg/journal/storage.go` | partial header/payload, stale head, ahead head, full corrupt frame tests | contract approved; code pending |
| `STACK-JRN-007` | write-before-effect step contract | `pkg/journal/operation.go` | mutation metadata and missing rollback-policy rejection tests | contract approved; code pending |
| `STACK-JRN-008` | interrupted-step recovery model | `pkg/journal/replay.go`, `pkg/journal/operation.go` | read-only retry and mutation recovery-required restart tests | contract approved; code pending |
| `STACK-JRN-009` | result/failure/rollback state machine | `pkg/journal/operation.go` | result idempotency, failure point, rollback and terminal tests | contract approved; code pending |
| `STACK-JRN-010` | cancellation boundary | `pkg/journal/operation.go` | before/during/after mutation cancellation tests | contract approved; code pending |
| `STACK-JRN-011` | non-disclosing error contract | all journal files | sentinel-error and hostile-value disclosure scan/tests | contract approved; code pending |
| `STACK-JRN-012` | authority boundary and non-goals | `pkg/journal/**`; CLI intentionally unchanged | source scan plus CLI regression tests | contract approved; code pending |

Tool limitations: Go race and unit tests cannot prove that storage hardware
honors flushes after reporting success. Evidence proves GOTTH Stack issues and
checks the documented Linux/Go synchronization operations in the required
order. Power-loss proof on every supported production filesystem remains a
downstream release gate.
