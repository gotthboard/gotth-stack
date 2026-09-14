# PostgreSQL runtime adapter implementation specification

## Package and API

Canonical private package:
`github.com/gotthboard/gotth-stack/internal/adapters/postgresql`.

```go
func Open(Options) (*Adapter, error)
func (a *Adapter) Preflight(context.Context, Request) (*Prepared, Summary, error)
func (a *Adapter) Stage(*Prepared) (Summary, error)
func (a *Adapter) ReconcileStage(operationID string) (Summary, State, error)
func (a *Adapter) Observe(context.Context, operationID string) (Observation, error)
func (a *Adapter) StopPrevious(context.Context, operationID string) (Summary, error)
func (a *Adapter) PreservePrevious(context.Context, operationID string) (Summary, error)
func (a *Adapter) CreateCandidate(context.Context, operationID string) (Summary, error)
func (a *Adapter) StartCandidate(context.Context, operationID string) (Summary, error)
func (a *Adapter) Verify(context.Context, operationID string, DesiredState) (Summary, error)
func (a *Adapter) StopCandidate(context.Context, operationID string) (Summary, error)
func (a *Adapter) RemoveCandidate(context.Context, operationID string) (Summary, error)
func (a *Adapter) RestorePrevious(context.Context, operationID string) (Summary, error)
func (a *Adapter) RestartPrevious(context.Context, operationID string) (Summary, error)
func (a *Adapter) RollbackStage(context.Context, operationID string) error
func (a *Adapter) Close() error
```

Every mutating method is one journalable engine call. `Prepared` is opaque and
instance-bound. Summaries contain only schema version, IDs, state enums, and
SHA-256 digests.

## Fixed roots and naming

`Options` binds absolute cleaned Docker-binary, state-root, data-root, and
secret-root paths; the exact Docker executable digest; one lowercase container
prefix; the literal engine endpoint `unix:///var/run/docker.sock`; and a
1–60 second timeout. Roots are opened and checked. The state root is `0700` and
owned by the effective user. Data and secret roots are not group/world writable
and are owned by root or the effective user.

For component `x`, the primary container is `<prefix>-x`, the data directory is
`<data-root>/x`, the password file is `<secret-root>/x-password`, and the
rollback container name is derived from the primary name plus the first twelve
hexadecimal characters of SHA-256(operation ID). Callers supply none of these
strings.

## Specification and secret

Component, database, and role IDs are lowercase ASCII slugs of at most 63
bytes. Image references are canonical printable repository names followed by
one lowercase `@sha256:<64 hex>` digest. UID/GID are positive and fit uint32;
the host port is 1–65535. The canonical JSON encoding of these fields is the
configuration digest.

The password file is a safely opened regular single-link file, 1–4096 bytes,
owned by root, the effective user, or the declared container UID, and not
group/world writable. Its content may not contain NUL. SHA-256 of the exact
bytes must equal `SecretRevisionDigest`. Metadata stores that digest and a
descriptor identity, never the bytes.

The data directory is a non-symlink directory owned by the container UID/GID
with mode `0700`. It may be empty or contain a `PG_VERSION` regular file whose
exact content is `17\n`. No other nonempty state is admitted.

## Fixed Docker boundary

The client endpoint is always passed as
`--host unix:///var/run/docker.sock`. The child environment contains only a
fixed `PATH`, private `HOME`, and private `DOCKER_CONFIG`. Process output is
bounded and discarded after strict JSON or fixed readiness parsing.

Create arguments are built by the package and enforce:

- exact digest-pinned image and derived container name;
- `--pull never`, so a missing image fails instead of opening a registry path;
- fixed ownership labels and candidate configuration/secret digests;
- `--user <uid>:<gid>`, `--read-only`, `--cap-drop ALL`, and
  `--security-opt no-new-privileges`;
- `127.0.0.1:<port>:5432/tcp` as the only publication;
- the derived data directory at `/var/lib/postgresql/data`;
- the derived password file read-only at `/run/secrets/postgres-password`;
- bounded `noexec,nosuid,nodev` tmpfs mounts for `/tmp` and
  `/var/run/postgresql`;
- only `POSTGRES_DB`, `POSTGRES_USER`, and
  `POSTGRES_PASSWORD_FILE=/run/secrets/postgres-password` environment values.

Image inspection requires the exact repository digest, `PG_MAJOR=17`, entrypoint
`docker-entrypoint.sh`, and command `postgres`. Effective container inspection
must match every field above and contain no undeclared bind, volume, port,
capability, security option, label in the adapter namespace, or environment
entry relevant to PostgreSQL initialization.

Readiness uses fixed `docker exec` calls to `pg_isready` and
`psql -AtX --set ON_ERROR_STOP=1` with one compiled query returning server major,
database, current user, and recovery state. The returned fields must equal the
staged spec and report PostgreSQL 17 primary mode.

## Durable transaction and state machine

Transactions use canonical `0600` `<operation-id>.json` files under a private
`0700` `transactions` directory. Stage writes, syncs, and atomically
renames a hidden sibling, then syncs the parent. Metadata binds adapter options,
engine identity, primary/rollback names, candidate and optional previous
specifications and image IDs, previous running state, data identity, secret
identity/revision, and a rollback-reference digest.

Observation classifies each container name as absent, previous, candidate, or
other and reports running/stopped separately. Mutations refuse `other` and
impossible two-name combinations. Exact successor state is idempotent. A
context cancellation or client failure after a mutating call returns a fixed
unknown/recovery-required class; only a later observation can select the next
action.

Rollback removes only the exact staged candidate container and never passes a
volume-removal flag. It restores and conditionally restarts the exact staged
previous container. Transaction cleanup is permitted only when runtime names
equal the preflight state. Data and secret files are never removed or rewritten.

## Verification

Unit seams cover process execution, image/container inspection, secret/data
descriptors, transaction write/sync/rename, and every state combination.
Linux integration compiles a test binary, runs it through an explicitly
selected local Docker client, and uses a private disposable data/secret/state
tree, random loopback port, and pinned PostgreSQL 17 image. It proves fresh and
managed-replacement rollback while retaining data across every container
replacement. No test names or inspects a live container.
