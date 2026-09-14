# PostgreSQL runtime adapter architecture

## Boundary

The adapter owns the lifecycle of one digest-pinned PostgreSQL 17 container.
It receives an approved non-secret specification and one password-file
revision. It does not receive a Compose document, Docker argument list, SQL,
database URL, application migration, backup destination, or another
component's state.

```text
controller + durable journal
  |
  | operation/component/config/secret revision
  v
component-scoped PostgreSQL adapter process
  +-- fixed image/container inspection --> local Docker engine
  +-- durable transaction staging ------> private adapter state
  +-- stop/rename/create/start ----------> one derived container name
  +-- fixed readiness/identity probes ---> PostgreSQL 17 container
  +-- reverse runtime rollback ----------> prior container identity
  +-- never delete ----------------------> PostgreSQL data directory
```

The Go package contains mutation mechanisms but is not imported by the
controller CLI. Docker-socket access is root-equivalent generic host authority;
therefore a production controller must not join the Docker group or open the
socket itself. Deployment of this adapter requires a separate component-
scoped process/grant whose installation contract remains downstream. This
slice proves the narrow mechanism and disposable runtime behavior; it does not
silently grant that mechanism to `gotth-stack apply`.

## Desired and effective state

Container, data-directory, and secret-file names are derived from one bounded
component ID under fixed roots. The caller chooses no host path or container
name. A canonical specification binds the exact image repository digest,
database, bootstrap role, UID/GID, and loopback port. The password bytes are
hashed from a safely opened descriptor and compared with the journal-approved
secret revision, but are never persisted or returned.

The image must already exist locally. Preflight inspects it and requires the
exact requested repository digest, `PG_MAJOR=17`, `docker-entrypoint.sh`, and
the `postgres` command. Pull policy and registry credentials are deliberately
outside this adapter.

An existing primary container is either absent or a complete adapter-managed
PostgreSQL container. Partial resemblance is not ownership. Labels, image,
user, environment, security settings, mounts, port publication, and command
must all normalize to the fixed shape. A managed replacement preserves every
durable identity field and may change only the digest-pinned PostgreSQL 17
image and approved configuration digest.

## Mutation and recovery model

The transaction records candidate and optional previous specs before mutation.
Runtime effects are separate calls: stop the previous container, rename it to
the operation-derived rollback name, create the candidate under the primary
name, start it, and verify it. Each call accepts only the exact predecessor or
idempotent successor state. Observation inspects both names so a crash between
any two calls is mechanically visible.

Rollback reverses runtime effects: stop and remove the exact candidate, rename
the exact previous container back, restart it only when it was previously
running, and verify it. A fresh-install rollback ends with no managed
container. It preserves the data directory even when the image entrypoint
initialized it. Deleting or rewinding database bytes would be dishonest;
product migrations and backup/restore remain separate downstream operations.

Only PostgreSQL 17-to-17 runtime replacement is admitted. Major upgrades,
database/role/port/UID changes, password rotation, and data-directory moves
require product-owned migration and recovery contracts and are rejected.

## Cost and failure model

Preflight performs bounded executable hashing, file checks, one engine query,
one candidate image inspection, optional previous image inspection, and up to
two container inspections. Observation performs the two container inspections.
Verification adds one `pg_isready` and one fixed read-only `psql` query inside
the container. No command uses a shell and no raw output crosses the public
error boundary.

`docker create`, `start`, `stop`, `rename`, and `rm` can return after the engine
has changed state but before the client reports success. The journal therefore
must treat every interrupted mutation as unknown until observation classifies
both container names. The adapter never retries an unobserved mutation and
never deletes volumes or host data.
