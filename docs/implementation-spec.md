# Implementation specification

## Plan-kernel public API

Canonical package: `github.com/gotthboard/gotth-stack/pkg/stack`.

```go
func ParseManifest(io.Reader) (Manifest, error)
func BuildPlan(Manifest) (Plan, error)
func MarshalPlan(Plan) ([]byte, error)
```

The module root contains no Go package. `cmd/gotth-stack` imports only the
canonical package.

## Bounds and validation

- Input: at most 1 MiB; exactly one JSON value; maximum nesting depth 32.
- Stack/component/secret IDs: lowercase ASCII slugs, 1–80 bytes.
- Adapter identity: `gotth-stack-adapter-<slug>.v<positive integer>`, at most
  160 bytes.
- Artifact source: a 1–2048 byte `https` or `oci` URL label with a host and
  path, and without user information, query, fragment, surrounding whitespace,
  or backslashes. It is an inert label in the plan-kernel workstream and is
  never opened or executed.
- Artifact and configuration digests: lowercase `sha256:` plus 64 hex digits.
- Components: 1–64. Dependencies/capabilities/secret slots: at most 64 each.
- Capability: dot-separated lowercase ASCII tokens, at most 160 bytes.

All set-like values must be unique. Component IDs are unique. Dependencies
must refer to another declared component. Kahn's algorithm with a sorted ready
set creates the stable order and detects cycles.

## Deterministic encoding

Normalization copies input before sorting. `encoding/json` marshals only fixed
structs—never maps—so field order is stable. Manifest digest hashes compact
normalized JSON. Plan digest hashes compact JSON with the digest field empty.
`MarshalPlan` emits indented JSON followed by one newline.
It rejects non-canonical `null` set fields and verifies that the manifest
digest can be reconstructed from the plan contents. These digests bind
content for identity and approval; they are not signatures and provide no
publisher authenticity.

## CLI

```text
gotth-stack validate <manifest.json>
gotth-stack plan <manifest.json>
```

The input path must name one regular, non-symlink file no larger than one MiB.
`validate` prints only `valid` and the manifest digest. `plan` writes the
deterministic plan only after full success. Usage/validation errors go to
stderr and return nonzero without echoing file contents or manifest values.

No `apply`, `exec`, plugin loading, network client, shell, Docker, systemd, or
privileged filesystem API exists in the plan kernel.

## Approval and recovery journal API

Canonical package: `github.com/gotthboard/gotth-stack/pkg/journal`.

```go
func Create(root, installationID string) (*Journal, error)
func Open(root, installationID string) (*Journal, Recovery, error)
func (j *Journal) RecordApproval(plan stack.Plan, input ApprovalInput) (Approval, error)
func (j *Journal) Approval(approvalID string) (Approval, error)
func (j *Journal) StartOperation(input OperationInput) (Operation, error)
func (j *Journal) BeginStep(input StepInput) (Operation, error)
func (j *Journal) FinishStep(input StepResultInput) (Operation, error)
func (j *Journal) Cancel(operationID string) (Operation, error)
func (j *Journal) BeginRollback(operationID string) (Operation, error)
func (j *Journal) FinishOperation(operationID string, outcome Outcome) (Operation, error)
func (j *Journal) Operation(operationID string) (Operation, error)
func (j *Journal) ActiveOperation() (Operation, bool, error)
func (j *Journal) Close() error
```

Public inputs contain bounded identifiers, an approval expiry, SHA-256 digests,
enumerated phases/modes/outcomes, and sorted secret-slot revision bindings.
They contain no secret value, command, environment, URL credential, adapter
payload, or raw rollback material. Returned values are deep copies.

`Create` makes a private journal directory and durable installation identity,
syncing both the journal directory and its trusted parent before success.
`Open` rejects an identity mismatch and takes a nonblocking exclusive Linux
`flock` on `journal.lock`. One process owns a journal at a time. The lock is
released by `Close` or process exit.

The public API never accepts an observation timestamp. A journal-owned clock
records UTC events and checks expiry. Same-package tests replace the private
clock; production callers cannot backdate an operation.

### Storage format

```text
<root>/
  installation.json  mode 0600, strict fixed JSON
  journal.log         mode 0600, append-only framed records
  journal.head        mode 0600, atomically replaced checkpoint
  journal.lock        mode 0600, advisory-lock target
```

The root is mode 0700. Every path is checked with `Lstat`; symlinks and
unexpected types fail closed. Existing overly broad modes are rejected rather
than silently repaired.

Frame version 1 is:

```text
8 bytes  magic "GTSJRN01"
4 bytes  big-endian JSON payload length
32 bytes SHA-256 of the payload
N bytes  compact canonical JSON payload
```

Payloads are at most 4 MiB so one approval can project every bounded plan
component without splitting its authority record. The log is at most 512 MiB
and 262,144 records.
The fixed record struct is marshaled without maps. Each payload has sequence,
previous digest, installation ID, timestamp, kind, and one event body. The
first record has sequence 1 and the all-zero previous digest. Subsequent
records increment by one and name the prior payload digest. Body issue/start/
finish times must equal the controller-observed envelope time.

The head contains schema version, final durable sequence, and digest. Head
replacement uses a same-directory temporary file, file sync, rename, and
directory sync. Temporary head files are never trusted as state.

### Approval binding

`RecordApproval` first calls `stack.MarshalPlan`, so a malformed or tampered
plan cannot be approved. It requires one `SecretRevision` for every
`component_id/secret_slot` pair and no extra or duplicate bindings. A revision
is a SHA-256 digest of secret-store revision metadata, not the secret. The
approval payload also projects the ordered component/adapter, artifact digest,
configuration digest, and capability set so audit does not depend on a later
manifest file.

Approval requires `approval_id`, `actor_id`, `authority_digest`, and an
`expires_at` strictly later than the journal-observed issue time.
`StartOperation` rejects expired approval, installation mismatch, plan
mismatch, reused IDs, or an approval already bound to another operation. Exact
duplicate approval and operation requests are idempotent. Step IDs are also
idempotency keys, and a mutation idempotency digest may name only one step
within an operation. A new operation is rejected while any prior operation in
the installation remains unfinished.

### Transition rules

Phases are `preflight`, `apply`, `verify`, and `rollback`. Modes are
`read_only` and `mutation`; apply and rollback require mutation mode, while
preflight requires read-only mode. One operation has at most one in-flight
step. Preflight precedes apply, and no apply step may begin after verification
begins. Attempts start at one and increase exactly by one for an explicitly
retried interrupted read-only step.

A mutation start requires an idempotency-key digest and either a
rollback-reference digest or an enumerated recovery-only reason. If a mutation
start has no durable finish record, replay reports `recovery_required` and no
API permits retry or cancellation. An unfinished read-only step reports
`retry_required`; the caller must append a new attempt for the same step,
component, and phase before performing it again. It cannot finish the
interrupted read-only attempt after restart. A mutation may append a
reconciliation result; the finish record marks that it resolves an interrupted
mutation so replay reconstructs the same state.

Every rollback step names the finished mutating step it compensates and must
use that step's component and rollback-reference digest. Recovery-only
mutations cannot be reported as rolled back. Rollback may be initiated after
rollback-eligible mutation without inventing a failure record.

`FinishStep` records `succeeded` with a result digest or `failed` with a
bounded reason code. Exact duplicate finish is idempotent; conflicting finish
fails. Failure stores the exact phase/component/step/attempt and permits only
rollback or an explicit terminal outcome. Ordinary `failed` is admissible only
before mutation begins. Once any mutation starts, a non-rolled-back operation
must terminate as `recovery_required` because external state may remain.

Cancellation is permitted only from approved or preflighting state with no
in-flight step and before any mutation-start record. `complete` requires no
in-flight step, no failure, a successful verify step for every approved
component, and no unresolved mutating step. `rolled_back` requires rollback to
have started, no in-flight step, no failed rollback step, and a successful
compensating step for every finished mutation that declared rollback
available. A failed mutation result is conservatively treated as possibly
partial external work.

`ActiveOperation` returns the sole unfinished operation, if present, and
`Approval` returns the immutable authority projection it names. Both return
deep copies. A restarted controller therefore discovers recovery work without
a second authority database or an unbounded history-list allocation.

### Filesystem and runtime contract

Supported evidence target is Linux 7.1 with Go 1.26.6 on local filesystems
providing Unix atomic same-directory rename, regular-file and directory
`fsync`, and advisory `flock`. Network filesystems and filesystems that reject
directory sync are unsupported and fail closed.

The implementation opens the journal directory through Go `os.Root` after
creation/validation and uses fixed relative names. The caller must place the
journal under a trusted parent; the alpha contract does not defend against a
privileged or same-account process deliberately replacing the supplied root
path before it is opened. The opened root and every fixed file must be owned by
the controller's effective UID in addition to having exact private modes.

Go `os.File.Sync` maps to the platform synchronization call. Linux `fsync(2)`
states that syncing a file does not make its directory entry durable; the
directory must also be synced. Linux `rename(2)` atomically replaces an
existing non-directory destination, and `flock(2)` releases a lock when all
descriptors for the open file description close or the process exits. The
journal relies on exactly those contracts and no stronger folklore.

Boundary tests cover frame size at limit-1, limit, limit+1, log length at the
configured bound, partial header/payload tails, stale and ahead heads, full
checksum corruption, sync/write/rename failures, lock contention, and reopen
after each durable checkpoint. A validated record count/digest/head triple is
the completeness oracle.

## Future apply state machine

```text
planned -> approved -> preflighting -> applying -> verifying -> complete
                                   \-> failed -> rolling_back -> rolled_back
                                                \-> recovery_required
```

The journal records and validates this state without executing adapters. An
`apply` command remains prohibited until adapter and disposable-runtime work
is admitted; no mutation stub is exposed.

## Caddy adapter

The private `internal/adapters/caddy` package implements the first platform
mechanism. Its exact API, bounds, fixed process calls, private transaction
format, atomic file sequence, runtime-digest observation, and rollback model
are specified in `caddy-adapter-implementation-spec.md`. It is not imported by
`cmd/gotth-stack`; admission of this package does not create an apply command.

## PostgreSQL runtime adapter

The private `internal/adapters/postgresql` package owns the exact API, bounds,
fixed Docker calls, durable transaction format, effective-container
inspection, readiness probes, runtime replacement, observation, and reverse
rollback specified in `postgresql-adapter-implementation-spec.md`. The
controller CLI does not import it. The package never pulls images, creates or
deletes secrets, deletes data, applies SQL migrations, changes Docker policy,
or exposes an `apply` command.

## Authentik runtime adapter

The private `internal/adapters/authentik` package owns the exact API, bounds,
fixed Docker calls, file-referenced secret binding, durable pair-replacement
state, effective server/worker inspection, health observation, recovery, and
reverse rollback specified in `authentik-adapter-implementation-spec.md`. The
controller CLI does not import it. The package never pulls images, mounts the
Docker socket, creates or rotates secrets, configures tenants/applications,
deletes persistent state, or exposes an `apply` command.

## Public website

Canonical executable: `cmd/gotthstack-web`.

Canonical private package: `internal/site`.

Canonical public organization URL:
`https://github.com/gotthboard`. The complete landing page renders
this as an ordinary anchor; it is not a server-side network dependency.

Pinned build inputs are Go 1.26.6, templ 0.3.1020, Node 26.7.0, npm 12.0.2,
Tailwind CSS and `@tailwindcss/cli` 4.3.3, and HTMX 2.0.10. Templ-generated Go,
minified CSS, and the pinned HTMX browser asset are committed. A release build
therefore uses `-mod=readonly` and reads no network or source asset directory.

### Routes

| Method | Path | Response |
| --- | --- | --- |
| `GET`, `HEAD` | `/` | complete landing page; `200` |
| `GET`, `HEAD` | `/principles?topic=control|trust|recovery` | HTMX fragment when `HX-Request` is exactly `true`, otherwise complete page; `200` |
| `GET`, `HEAD` | `/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css` | embedded minified CSS; immutable cache |
| `GET`, `HEAD` | `/static/htmx-2.0.10.min.js` | embedded pinned HTMX; immutable cache |
| `GET`, `HEAD` | `/healthz` | `text/plain`; fixed `ok\n` |

The missing topic selects `control`. Duplicate, empty, or unknown topic values
return `404`. Non-GET/HEAD methods return `405` with `Allow: GET, HEAD`.
Unknown paths return `404`. Redirects are not used.

The handler rejects empty, escaped, dot-segment, and repeated-slash paths
before Go's `ServeMux` can canonicalize them with a redirect. Go's general
`OPTIONS *` shortcut is disabled so every response crosses the site handler
and security-header boundary. Request headers, including the request line, are
limited to one MiB.

All responses set `X-Content-Type-Options: nosniff`, `Referrer-Policy:
no-referrer`, `X-Frame-Options: DENY`, and `Permissions-Policy` denying camera,
microphone, and geolocation. HTML adds a content security policy allowing only
same-origin scripts and styles and forbidding objects, frames, and base-URI
rewrites. HTML uses `Cache-Control: no-store`; static assets use
`public, max-age=31536000, immutable`.

The principles lookup is a fixed three-entry slice scanned linearly. With
`p=3` fixed topics, selection is constant time and space. A future unbounded
content system must replace the fixed assumption explicitly rather than hide
database or allocation cost behind this handler.
