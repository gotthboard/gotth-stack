# Approval and recovery journal hardening findings

Reviewed source range: `e6b6e8f..4a140590ed90472caad93d9d526fd4bbc1d89eb2`

The initial implementation was not admissible. Cold review found the following
real defects and required corrections before verification:

1. Caller-supplied observation times could backdate authority. The journal now
   owns issue/start/finish time and requires body times to equal envelope time.
2. A 1 MiB record could not hold the largest valid bounded plan projection.
   The record bound is 4 MiB and a largest-valid plan test proves the complete
   approval fits without splitting authority.
3. Path-level checks could race descriptor opening. The opened root, lock, log,
   head, and installation descriptors are checked for type, exact mode, and
   effective-UID ownership; fixed files use `O_NOFOLLOW`.
4. A failed final create/open could leave a corrupt new journal directory.
   Create now removes the newly created directory unless the common Open path
   succeeds.
5. Interrupted read-only retry and mutation reconciliation had live/replay
   divergence. Durable retry/reconciliation markers now reconstruct the same
   state after another restart.
6. A failed mutation could be treated as having changed nothing. Failed
   mutation remains rollback-eligible, and any operation that started mutation
   may terminate only rolled back or recovery-required.
7. Rollback required a fabricated failure after a successful mutation.
   Operator-initiated rollback is now legal whenever rollback-eligible mutation
   exists and no step is in flight.
8. One approval could authorize multiple operation IDs. An approval is now
   single-operation authority; exact duplicate start keeps the original ID.
9. Separately approved unfinished operations could race one installation. The
   journal now admits at most one unfinished operation.
10. Restart recovery required a second database to remember the active ID.
    `ActiveOperation` and immutable `Approval` lookup expose the needed state
    without an allocation-heavy history-list API.
11. The canonical public API list omitted those two restart lookup methods.
    The implementation specification now names `ActiveOperation` and
    `Approval`, matching the tested public surface; runtime behavior is
    unchanged.

Corrections were committed as `8a9333c`, `a4a5366`, `795e9db`, and `4a14059`.
No adapter, executor, apply command, network client, secret value, deployment,
tag, or release was added while correcting these defects.
