# Caddy adapter implementation specification

## Package and public surface

Canonical private package:
`github.com/gotthboard/gotth-stack/internal/adapters/caddy`.

```go
func Open(Options) (*Adapter, error)
func (a *Adapter) Preflight(context.Context, Request) (*Prepared, Summary, error)
func (a *Adapter) Stage(*Prepared) (Summary, error)
func (a *Adapter) ReconcileStage(operationID string) (Summary, State, error)
func (a *Adapter) RollbackStage(context.Context, operationID string) error
func (a *Adapter) Observe(context.Context, operationID string) (Observation, error)
func (a *Adapter) Install(operationID string) (Summary, error)
func (a *Adapter) Activate(context.Context, operationID string) (Summary, error)
func (a *Adapter) Verify(context.Context, operationID string, DesiredState) (Summary, error)
func (a *Adapter) Restore(operationID string) (Summary, error)
func (a *Adapter) Reactivate(context.Context, operationID string) (Summary, error)
func (a *Adapter) Close() error
```

`Prepared` is opaque outside the package and binds the adapter instance,
operation/component IDs, target descriptor identity, raw previous/candidate
bytes, file mode, canonical adapted candidate JSON, and preflight runtime
digest. Exported summaries contain only schema version, operation/component
IDs, SHA-256 digests, and enumerated state. Returned values never contain raw
configuration, command output, path, or URL.

## Bounds and validation

- Operation and component IDs: lowercase ASCII slug, 1–80 bytes.
- Caddyfile: 1–1,048,576 bytes, valid UTF-8, no NUL.
- Adapted or running JSON: 1–4,194,304 bytes and exactly one valid JSON value.
- Caddy process stderr and non-adaptation stdout: independently capped at
  64 KiB and always discarded after classifying success/failure. Adapted JSON
  stdout is capped at the four MiB JSON limit.
- Executable/config/state/working-directory paths: absolute and cleaned; the
  mutable state path does not overlap the executable or Caddyfile. The
  executable is at most 256 MiB and its SHA-256 digest must equal the required
  `CaddyBinaryDigest` on open and immediately before every command.
- Admin address: `http://127.0.0.0/8:<1-65535>` or
  `http://[::1]:<1-65535>`, no credentials, path, query, or fragment.
- Previous and candidate adapted JSON must both contain an enabled
  `admin.listen` exactly equal to that configured address.
- Caddyfile adapter name is fixed to `caddyfile`.

Input configuration digest is lowercase `sha256:` plus 64 hexadecimal bytes
and must equal the candidate bytes before any process starts. A bare `import`
token outside quoted values, and either documented environment placeholder
form anywhere outside comments, reject.

## Fixed process calls

```text
<caddy> adapt --config <private-scratch>/candidate --adapter caddyfile
<caddy> validate --config <private-scratch>/candidate --adapter caddyfile
<caddy> reload --config <staged-exact-file> --adapter caddyfile --address <host:port>
```

Arguments are constructed by the package, never accepted from the caller.
The child receives only `PATH`, private `HOME`, `XDG_CONFIG_HOME`, and
`XDG_DATA_HOME`; proxy and application environment are absent. `cmd.Dir` is
the validated, binding-covered `WorkingDirectory` that must match the running
Caddy service. Context
deadline/cancellation terminates the child through `os/exec`.

## Durable transaction layout

```text
<state-root>/
  transactions/
    <operation-id>/
      metadata.json       0600 canonical fixed-struct JSON
      previous.caddyfile  0600
      candidate.caddyfile 0600
```

The state root and `transactions` are `0700`. Stage writes a fixed hidden
temporary sibling directory, syncs each file, syncs that directory, renames it
to the final operation name, and syncs `transactions`. The metadata file is
written last. Existing final state is decoded canonically and all file digests
are rechecked. An exact match is idempotent; mismatch is a conflict. Temporary
state is observable as incomplete and never silently promoted.
Explicit reconciliation may promote a fully validated temporary transaction.
Explicit staging rollback removes incomplete private state, or removes a
committed transaction only while file and runtime state still match the
recorded preflight baseline.

## Persistent file replacement

The config parent is held through `os.Root`. Before each replacement, the
opened target descriptor must still be a safe regular file owned by the
effective user and must hash to an admitted state. A fixed operation-derived
temporary sibling is opened with create-exclusive and no-follow semantics,
written fully, synced, closed, renamed over the target, then the config parent
is synced. The temporary descriptor is assigned the recorded UID, GID, and
mode explicitly before writing, so umask cannot narrow service readability;
the final descriptor is reopened and checked after rename. Stale temporary
state makes recovery explicit rather than being silently removed during
apply. Observation reports it separately. Explicit restore validates, removes,
and directory-syncs safe failed-install residue; verification requires no
temporary file.

## Runtime observation

The package uses an `http.Client` with a direct transport, no proxy, redirects
disabled, fixed timeouts, and the configured literal loopback address. Only
`GET /config/` is sent. The body is bounded, decoded as one JSON value, and
re-encoded canonically before hashing. Non-2xx status, bad content, oversized
body, redirects, and transport errors return fixed failure classes.

Preflight hashes canonical output from `caddy adapt`; this is the expected
runtime candidate digest. It separately records the canonical preflight
`GET /config/` digest as the previous runtime state. Reload success is not
sufficient: activation and rollback query the API and compare exact digests.

## Test seams

Private interfaces wrap process execution, Admin API reads, file write/sync/
rename, and directory sync. Production defaults are direct mechanisms.
Unit tests inject every failure checkpoint and all state combinations. A
Linux-only disposable integration test starts Caddy with private storage and a
random loopback Admin API/listener, exercises candidate install/activation and
rollback across adapter reopen, and proves the unrelated route remains live.
It is skipped unless `GOTTH_CADDY_BIN` names the explicitly selected binary.

No source in this slice imports `pkg/journal` or adds an apply command. The
future controller must pair each adapter mutation with the already-admitted
journal state machine; the adapter does not invent authority for itself.
