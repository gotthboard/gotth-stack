# Authentik runtime adapter architecture

## Boundary

The adapter owns one Authentik 2026.5 server/worker pair. It receives one
approved non-secret specification and revision digests for an existing
PostgreSQL password and Authentik secret key. It does not own PostgreSQL,
create secrets, configure tenants or applications, mount the Docker socket,
pull images, or grant the controller an `apply` path.

```text
controller + durable journal
  |
  | operation/component/configuration/two secret revisions
  v
component-scoped Authentik adapter process
  +-- fixed image/container inspection --> local Docker engine
  +-- durable transaction staging ------> private adapter state
  +-- stop/rename/create/start ----------> derived server and worker names
  +-- fixed health probes ---------------> both Authentik roles
  +-- reverse runtime rollback ----------> exact prior pair
  +-- never delete ----------------------> Authentik data/certs/templates
```

Docker-socket access is root-equivalent generic host authority. A future
installation must place this adapter behind a component-scoped grant; the
controller process must not join the Docker group. This package is private and
is not imported by the CLI.

## Documented runtime contract

Authentik 2026.5 requires PostgreSQL for application data, sessions, task
coordination, `LISTEN` channels, and advisory locks. This adapter therefore
uses a direct PostgreSQL endpoint and does not admit transaction-pooling
proxies. Authentik configuration values support `file://` references. The two
secret-valued settings point at read-only mounted files, so secret bytes never
enter Docker arguments, environment inspection, transaction state, errors, or
evidence.

The official container runs `dumb-init -- ak` and exposes separate `server`
and `worker` roles. The pair uses host networking solely to reach the
PostgreSQL adapter's loopback publication. HTTP, HTTPS, and metrics listeners
are explicitly bound to distinct loopback ports. The worker has no Docker
socket, capabilities, privileged mode, or outpost-management authority;
outposts must be deployed separately.

## Desired and effective state

Container and durable path names are derived from one bounded component ID.
The canonical specification binds the exact repository digest, direct
PostgreSQL database/role/loopback port, six distinct role-specific Authentik
loopback listener ports, and non-root UID/GID. The requested image must already exist,
carry an Authentik 2026.5 version label, and have the documented entrypoint.

Both roles share a persistent data directory and read-only templates. The
worker additionally receives the persistent certificates directory. Directory
bindings preserve device, inode, ownership, and mode while allowing Authentik
to add files and subdirectories inside those persistent roots. Their parent is
a private adapter-owned directory, preventing an unprivileged runtime UID from
renaming the bound children between validation and container creation. The root
filesystem is read-only, capabilities are dropped, privilege escalation is
disabled, `/tmp` is a bounded tmpfs, and shared memory is explicitly bounded.
Effective inspection rejects every undeclared Authentik environment setting,
adapter label, mount, network mode, capability, or command.

## Mutation and recovery model

Preflight observes the complete pair. It accepts only both roles absent or a
coherent managed pair with identical durable identity. Transaction metadata is
made durable before mutation. Each container stop, rename, create, start, and
remove is a separate public method and therefore a separate journalable
effect. Role order is explicit: stop worker then server; preserve both; create
server then worker; start and health-check server before starting the worker;
then verify the pair.

Rollback reverses those effects: stop worker then server, remove the exact
candidate pair, restore the exact previous pair, restart the server before the
worker when those roles were previously running, and verify the restored
state. A fresh-install rollback ends with no containers. Persistent data,
certificates, templates, and secret files are never deleted or rewritten.

Docker client failure after a mutating call has an unknown outcome. The
adapter returns recovery-required and a reopened caller must inspect all four
primary/rollback names before selecting the next idempotent action. Partial
pairs, mixed generations, foreign containers, and unbound filesystem or
engine changes fail closed.

## Cost model

Preflight hashes the fixed Docker client and two small secret files, checks
three durable directories, inspects the engine and candidate image, and
performs bounded inspection of four derived container names. Observation is
four bounded container inspections. Verification adds two fixed `ak
healthcheck` processes. No shell is invoked and no raw process output crosses
the public error boundary.
