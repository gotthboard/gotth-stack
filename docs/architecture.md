# Architecture

## Authority split

```text
operator/browser
      |
      v
gotth-stack controller
  plan / approval / journal / audit / recovery
      |
      +--> Caddy adapter
      +--> Authentik adapter
      +--> PostgreSQL adapter
      +--> GOTTH Mail adapter
      +--> mail front/proxy adapter
      +--> Postfix adapter
      +--> Dovecot adapter
      +--> Rspamd adapter
      +--> GOTTH Board adapter
      +--> admitted gotth-extension-* provider
```

The controller is the only composition authority. Adapters receive only the
component-specific target and capability grant needed for one operation. They
do not receive another component's secrets, administrator session, database,
or rollback state.

The admitted plan kernel implements only the upper-left planning boundary. It parses a secret-free
manifest, normalizes unordered declarations, validates the dependency graph,
and emits a deterministic plan. It does not load adapters or inspect a host.

## Manifest and plan identity

The manifest binds each component to:

- one stable component ID;
- one versioned adapter identity;
- one artifact source label and exact SHA-256 content digest;
- one exact configuration digest;
- explicit dependencies;
- explicit requested capabilities; and
- named secret slots without values.

The normalized manifest sorts components and every set-like field. Its SHA-256
digest is the desired-state identity. The planner performs a stable
topological sort and hashes the resulting plan without its own digest field.
The human-readable source label is inert in the plan-kernel workstream; a
future adapter admission layer must validate and retrieve it without treating
text as a command.
The digests detect inconsistent or changed content; they are not signatures
and do not establish artifact publisher authenticity.

## Failure model

- Invalid or ambiguous input produces no plan.
- A dependency cycle produces no partial order.
- Planner output is constructed in memory and written only after success.
- Rejection errors use fixed reason codes/messages and never interpolate input
  values. Successful plans intentionally display the accepted non-secret
  component, adapter, artifact-source, capability, and secret-slot identifiers
  that the operator is being asked to approve.
- Repeated validation or planning is side-effect free and byte-identical.

Future apply cannot simply call adapters in a loop. It requires the approval
and recovery journal below, adapter preflight and recovery contracts, and an
explicit unknown-outcome state. If rollback cannot be proven, the UI must say
so instead of fabricating a green button.

## Approval and recovery journal

The journal is a Linux-local authority record, not an adapter executor. One
opened journal owns an advisory exclusive lock for its lifetime. Its directory
contains only a stable installation identity, an append-only framed log, a
small durable head checkpoint, and the lock file. The directory and files are
private to the controller account; symlinks and unexpected file types are
rejected.

Each record contains a schema version, monotonically increasing sequence,
previous-record digest, controller-observed UTC time, installation ID, event
kind, and one strictly validated event payload. The frame carries a fixed magic/version,
bounded payload length, and SHA-256 payload checksum. The payload digest is the
next record's chain link. Time is evidence only; sequence and hashes establish
ordering because wall clocks can move backward.

Append durability is deliberately expensive and explicit:

1. validate the transition against the reconstructed state;
2. append the complete frame to the locked log;
3. `fsync` the log;
4. write and `fsync` a replacement head in the same directory;
5. atomically rename the head into place; and
6. `fsync` the containing directory.

Success is not returned before all six steps succeed. Failure leaves the
caller without authority to act. On reopen, a partial final frame may be
discarded only when the durable head still identifies the preceding valid
record. A valid extra frame after an older head represents a crash between log
and head synchronization and may repair the head forward. A head ahead of the
log, a fully framed checksum failure, a chain break, a sequence gap, or an
invalid transition is corruption and blocks operation.

Approval binds the exact plan plus the selected installation, actor assertion,
authenticated-authority evidence digest, expiry, and secret-slot revision
digests. Secret material is never copied into the journal. Caller-supplied
approval, operation, and step IDs are idempotency keys; mutation idempotency
digests are unique within an operation. One approval binds exactly one
operation ID. Exact duplicates are stable, while conflicting reuse is rejected.

Callers do not supply journal observation times. A controller-owned clock
records issue, transition, and result times and evaluates approval expiry, so a
request cannot revive an expired approval by backdating an operation.

The journal distinguishes read-only inspection from mutation. An inspection
interrupted before its result can be explicitly retried with a higher attempt.
A mutation is different: its start record is durable before the caller could
act. If no result follows, restart cannot know whether the external action
happened. The reconstructed operation therefore becomes `recovery_required`;
blind retry and ordinary cancellation are forbidden. A reconciled durable
result resolves the unknown outcome; otherwise the operation remains
explicitly recovery-required. A rollback step names the finished mutating step
it compensates, including a failed mutation that may have partially changed
external state. Rollback references are non-secret digests or explicit
recovery-only reason codes, never arbitrary commands or payloads. An ordinary
terminal failure is forbidden after any mutation starts; unresolved or
intentionally uncompensated external state remains recovery-required.
Rollback may be operator-initiated after a successful mutation; it does not
require a fabricated later failure.

The journal scans linearly during open and retains reconstructed operation
summaries in memory. The alpha format caps frame and log sizes. This is honest
and auditable for initial deployments; segmentation or indexing is deferred
until measured operation history justifies it.

## Operation state machine

```text
approved -> preflighting -> applying -> verifying -> complete
    |             |             |            |
    +-> cancelled +-> failed ---+------------+
                                  \
                                   -> rolling_back -> rolled_back
                                   -> recovery_required
```

Only one step may be in flight per operation. Cancellation is legal only
before any mutation starts. Failure records preserve the exact phase,
component, step, attempt, and bounded reason code. Rollback is a separate
phase with the same write-before-effect and unknown-outcome rules.

The journal does not claim to authenticate actors itself. It records the exact
actor assertion and authority-evidence digest presented by the future
authenticated controller boundary. That boundary remains downstream.

## Integration boundaries

`gotth-infrastructure`, `gotth-authentik`, `gotth-release`, and
`gotth-pg-migrate` remain independently versioned mechanism libraries. Future
adapters may pin them exactly; `gotth-stack` does not absorb or duplicate their
policy. `gotth-extensions` defines extension compatibility, while a concrete
`gotth-extension-*` repository supplies one provider mechanism. Neither grants
deployment authority by naming convention.

## Mail runtime topology

Mailu is not deployed by `gotth-stack`. GOTTH Mail owns the mail control plane
and canonical domain/mailbox/alias state. Separate adapters manage its
front/proxy, Postfix, Dovecot, and Rspamd processes without turning those core
daemon contracts into provider extensions.

Shared Caddy may serve the GOTTH Mail administrator and custom webmail HTTP
routes alongside other domains. It does not replace the mail-specific ingress
or proxy ordinary SMTP/IMAP traffic. Before V3 can apply anything, its topology
contract must assign each public port, certificate, ACME challenge, and
hostname to exactly one owner; Caddy and mail front/proxy may not both claim the
same listener or certificate lifecycle.

The deployment graph may order Authentik configuration before GOTTH Mail
identity wiring, but that ordering does not create a runtime availability
dependency. Postfix delivery, Dovecot access/lookup, and Rspamd decisions must
continue when Authentik is unavailable. A shared mail runtime may host multiple
domains, with per-domain DNS, DKIM, TLS, policy, alias, and mailbox state.
