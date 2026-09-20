# Cold review 1

## Decision

CLEAN.

## Review

The candidate has five typed constructors over one unexported fixed Docker
mechanism. There is no caller-selected role, argument vector, environment,
shell, image pull, Docker-socket mount, volume deletion, or generic apply
surface. Each mutation checks the pinned Docker binary, network, image,
configuration, secret revision, mount identity, and durable transaction before
acting. Mixed or ambiguous runtime state returns recovery-required rather than
guessing.

The review traced fresh install, replacement, restart/reopen, commit, reverse
rollback, duplicate calls, stopped prior state, incomplete staging, binding
replacement, and hostile container/image metadata. Persistent role state is
mounted independently and is never removed by the adapter.

The prior cached-image evidence was rejected. The replacement proof now uses
Mail source `12dbe59973cba53e52d56437f21424b2ec53f3c8` and exact Rspamd digest
`sha256:fe7b4f7a3db8ce9c453ad7585e1a752fbd109b5d36d7a35c2c236bd33d457fca`.
The manifest/parser mismatch was repaired by adding the required positive build
epoch to Stack's closed schema; unknown fields remain rejected.

No actionable defect remains in this child. Controller authority and complete
topology acceptance remain outside scope and visibly unfinished.
