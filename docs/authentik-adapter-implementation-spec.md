# Authentik runtime adapter implementation specification

## Package and API

Canonical private package:
`github.com/gotthboard/gotth-stack/internal/adapters/authentik`.

```go
func Open(Options) (*Adapter, error)
func (a *Adapter) Preflight(context.Context, Request) (*Prepared, Summary, error)
func (a *Adapter) Stage(*Prepared) (Summary, error)
func (a *Adapter) ReconcileStage(operationID string) (Summary, State, error)
func (a *Adapter) Observe(context.Context, operationID string) (Observation, error)
func (a *Adapter) StopPrevious(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) PreservePrevious(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) CreateCandidate(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) StartCandidate(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) VerifyRole(context.Context, operationID string, Role, DesiredState) (Summary, error)
func (a *Adapter) StopCandidate(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) RemoveCandidate(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) RestorePrevious(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) RestartPrevious(context.Context, operationID string, Role) (Summary, error)
func (a *Adapter) Verify(context.Context, operationID string, DesiredState) (Summary, error)
func (a *Adapter) RollbackStage(context.Context, operationID string) error
func (a *Adapter) Close() error
```

`Role` is exactly `server` or `worker`. Every mutating method performs at most
one fixed Docker mutation. Prepared state is opaque and instance-bound.
Public results contain only bounded identifiers, enums, booleans, and SHA-256
digests.

## Fixed roots, names, and input

`Options` binds a root-owned absolute Docker client and digest, private state,
data, and secret roots, a lowercase container prefix, the literal
`unix:///var/run/docker.sock`, and a 1-60 second command timeout. The data root
contains derived `<component>/data`, `<component>/certs`, and
`<component>/templates` directories. The secret root contains derived
`<component>-postgresql-password` and `<component>-secret-key` files.

IDs are lowercase ASCII slugs of at most 63 bytes. Images are canonical
repository names plus one lowercase SHA-256 digest. Database host is exactly
`127.0.0.1`; database, role, and all ports are bound by the configuration
digest. Both roles' HTTP, HTTPS, and metrics ports plus PostgreSQL are all
distinct. UID/GID are positive and the UID is non-root.

Secret files are safely opened regular single-link files of 1-4096 bytes,
owned by root, the adapter user, or the declared UID, readable by the declared
UID, and not group/world writable. NUL, CR, and LF are forbidden. The exact
bytes must hash to their approved revision digests, and the Authentik secret
key must contain at least 50 bytes. Metadata retains only
digests and descriptor identities.

The component directory is a private, non-symlink adapter-owned directory.
Durable child directories are non-symlink directories owned by the declared UID/GID.
Data and certs are mode `0700`; templates are mode `0700` and mounted
read-only. Device, inode, ownership, and mode are transaction-bound; link count
and directory size are deliberately not bound because valid runtime-created
children change them.

## Fixed Docker shape

The candidate image must already be local. Inspection requires the exact
repository digest and image ID, version label `refs/tags/version/2026.5.*`,
user `1000`, and entrypoint `dumb-init -- ak`. Create always passes `--pull
never`, a derived role name, fixed ownership labels, `--network host`, the
declared non-root user, read-only root, all capabilities dropped,
no-new-privileges, a 64 MiB noexec/nosuid/nodev `/tmp`, and 512 MiB shared
memory. No Docker socket is mounted.

Both roles mount data at `/data`, templates read-only at `/templates`, and the
two secret files read-only below `/run/secrets`. The worker alone mounts certs
at `/certs`. Environment is fixed to the direct loopback PostgreSQL endpoint,
`file://` references for password and secret key, disabled update/error
reporting, and role-specific loopback listener values. Commands are exactly
`server` and `worker`.

Effective inspection must reproduce that shape exactly. It rejects extra
adapter labels, `AUTHENTIK_*` or `POSTGRES_*` controls, mounts, publications,
capabilities, security options, and non-host networking.

## Durable transaction and recovery

Transactions are canonical `0600` JSON files under a private `0700`
transaction directory. Staging uses a same-directory `0600` temporary file,
sync, atomic rename, and directory sync. Metadata binds adapter options,
engine identity, image identity, all four container names, candidate and
optional previous specifications, prior running states, directory identities,
both secret identities/revisions, and a rollback-reference digest.

Observation reports each server/worker primary and rollback state plus power.
Mutations admit only the exact predecessor or exact idempotent successor;
every mixed or foreign state returns recovery-required. Staging cleanup is
permitted only after runtime names equal the complete preflight state.
Persistent and secret paths are never removed or modified.

## Verification

Unit seams cover input, executable, image/effective inspection, directories,
secrets, durable staging, command construction, role ordering, every reachable
forward/reverse transition, unknown outcomes, and reopen. The Linux disposable
test uses an already-present pinned Authentik 2026.5.2 image and a private
PostgreSQL 17 fixture on loopback. It proves fresh start, both health checks,
reopen, configuration-to-configuration managed replacement, reverse rollback,
and persistent-state survival without naming or inspecting live services.
