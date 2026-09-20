# Cold review 1

## Decision

CLEAN after correction.

## Review

The first pass rejected four real defects: capability subsets could authorize
the full fixed action list; artifact/configuration digests were recorded but
not compared to the typed request; verifier failure could be masked by a
coarse candidate observation; and recovery-only work could be mixed with
rollbackable mutations. It also found incomplete recovered-failure handling
and terminal duplicate execution that violated journal idempotency.

The final source requires exact authority equality, isolates recovery-only
plans, requires successful verification plus exact observed state, seals
post-network verification failure as recovery-required, completes recovered
read-only failures in one recovery call, and returns durable terminal results
for exact duplicate execution. Power/state predicates now reject internally
inconsistent absent states. No actionable defect remains.
