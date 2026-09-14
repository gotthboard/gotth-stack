# Caddy adapter architecture

## Boundary

The controller remains the composition and authority owner. The Caddy adapter
receives one already-composed, secret-free, complete Caddyfile and an approved
digest. It does not accept route fragments, shell text, arbitrary arguments,
environment values, systemd authority, or another component's state.

```text
controller + durable journal
  |
  | preflight (read-only live state)
  v
Caddy adapter
  +-- fixed caddy adapt/validate --> isolated private scratch
  +-- durable stage -------------> private transaction directory
  +-- atomic install ------------> configured complete Caddyfile
  +-- fixed caddy reload --------> loopback Caddy Admin API
  +-- inspect/verify ------------> file digest + GET /config/
  +-- restore/reactivate --------> exact staged previous state
```

There are three state domains: adapter transaction state, the persistent
Caddyfile, and Caddy's running JSON configuration. Treating them as one
mutation would make crash recovery dishonest. The API therefore exposes
staging, file replacement, and runtime activation separately. The controller
must durably begin a journal mutation step before each call.

## Documented Caddy behavior

`caddy validate` loads and provisions a config without starting it.
`caddy reload --config` adapts the named Caddyfile and sends the result to the
Admin API; the command is equivalent to `POST /load`. Caddy documents `/load`
as an atomic replacement: on provisioning failure the old active config stays
active, and success waits for the reload to finish. `GET /config/` returns the
running JSON config. The adapter relies on these contracts and nothing
stronger.

Caddy's autosave is not the persistent source of truth for the standard
`caddy.service`, which starts from `/etc/caddy/Caddyfile`. Consequently the
adapter must persist the file before runtime activation. API-only mutation
would work until the next service restart and is rejected.

## Least privilege and secret boundary

The executable, config parent, config file, and state directory are opened and
checked rather than trusted by path string alone. The executable is hashed on
open and before every command and must equal the approved artifact digest. The
executable and current config must be regular, non-symlink files owned by root
or the effective user as applicable and not group/world writable. The state
directory is owned by the effective user with mode `0700`. The Admin API must
be a literal loopback address over plain HTTP;
DNS names, HTTPS, Unix sockets, redirects, proxies, and ambient proxy
environment are not admitted in this slice.

Preflight requires the adapted previous and candidate documents to contain the
same explicit Admin API listener. A candidate that disables or moves the
listener is rejected before staging; otherwise a successful reload could cut
off the verification and rollback mechanism.

The adapter launches only the fixed Caddy binary with fixed verbs and flags;
there is no shell. Child environment is rebuilt from a small allowlist and
uses operation-private HOME/XDG directories. Every command uses one validated,
binding-covered working directory matching the service, so relative Caddy
configuration paths do not acquire ambient controller-process semantics.
Caddyfile `import`, `{$NAME}` and
`{env.NAME}` forms are rejected. This blocks ambient files and environment
from widening the approved configuration. The caller is still responsible for
composing a configuration without inline credentials; arbitrary Caddy modules
make generic secret detection impossible. Config bytes remain private adapter
state and are never returned or interpolated into errors.

## Transaction and recovery model

Preflight reads the previous Caddyfile and running Admin API config, adapts and
validates the candidate, and produces an opaque prepared value. Durable stage
creates an operation-specific transaction directory with fixed `0600` files,
syncs each file and the directory, atomically renames the directory into place,
then syncs the state root. Its canonical metadata binds operation/component,
target identity, previous/candidate file digests, and previous/candidate
runtime digests. The metadata digest is the journal rollback reference.

Atomic install accepts exactly two target states: previous or candidate.
Previous is replaced; candidate is an idempotent success. Anything else is
`recovery_required`. Activation likewise accepts only the previous or
candidate running digest. Candidate is success; previous may be reloaded;
anything else refuses mutation.

Rollback reverses the effects. Restore accepts candidate or previous file
state and atomically writes the staged previous bytes. Reactivation accepts
candidate or previous runtime state and reloads the restored file. Verification
requires the exact previous file and runtime digests. An observation API makes
all four state classes visible without returning raw material, enabling the
journal to reconcile an interrupted mutating step.

## Failure and cost model

Each preflight performs three bounded Caddy processes (adapt previous, adapt
candidate, validate candidate) and one Admin API read.
Each activation performs at most one reload process and two API reads. Staging
writes two bounded configurations plus small metadata once. Atomic file
replacement writes one complete configuration; Caddy configuration is small
and whole-file replacement is intentionally preferred to fragile partial API
mutation.

A process crash may leave a fixed temporary file or staging directory. Neither
is trusted as committed state. Observation reports a persistent-file temporary
separately; verification requires it absent, and explicit restore removes and
directory-syncs safe residue before reporting rollback success. A final
transaction directory, target file, or running config is classified by digest
before any recovery action. Hardware
may still lie about successful flushes; tests prove requested syscall order,
not impossible universal power-loss behavior.

A complete temporary staging directory may be promoted by explicit
reconciliation. A partial staging directory remains recovery-required until an
explicit staging rollback removes it. A committed transaction can be removed
only while both persistent and runtime Caddy state still equal the recorded
preflight baseline, so cleanup cannot destroy rollback material after install.
