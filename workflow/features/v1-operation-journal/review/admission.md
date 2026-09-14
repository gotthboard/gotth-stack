# Approval and recovery journal admission

## Decision

Accept with constraints.

## Problem

Future stack mutations need one durable authority record that proves the exact
reviewed plan, explicit approval, operation progress, failure point, rollback
information, and restart decision without pretending an interrupted mutation
is safe to retry.

## Admissibility

The implementation is reviewable, bounded, locally durable under the
documented Linux filesystem contract, strict under hostile replay, explicit
about unknown outcomes, and isolated from all execution authority. Exact code
source `4a140590ed90472caad93d9d526fd4bbc1d89eb2` passed the recorded
development-host gates and two clean cold passes.

## Judge notes

The initial implementation was not admitted. Eleven real defects were
corrected, including caller-controlled time, an invalid record bound,
path/descriptor races, replay divergence, unsafe mutation failure semantics,
approval replay, concurrent operations, missing restart discovery, and stale
API documentation. The resulting mechanism is direct and has no fake generic
executor abstraction.

## Operator notes

The package writes only its caller-selected private local journal directory.
It has no adapter, apply, network, shell, Docker, systemd, secret retrieval,
host mutation, or deployment path. Storage failures poison the handle and
require reopen/replay. Rollback of this unreleased slice is a commit revert and
removal of unconsumed test journals.

## Documentation notes

The design follows Go 1.26.6 file API behavior and Linux 7.1 `fsync(2)`,
`rename(2)`, and `flock(2)` contracts. Documentation does not claim hardware
power-loss proof beyond successful synchronization calls on the tested ZFS
filesystem.

## Userspace notes

Current validate/plan behavior is preserved. Release naming remains
`1.0.0-alpha.N`; the historical `v1` identifier is only a workstream ID.

## Conditions

- Do not add or expose `apply` until platform adapters, secret handling,
  disposable integration, and rollback proofs are separately admitted.
- Keep the on-disk format unstable and unreleased on the alpha line until an
  upgrade/migration contract exists.
- Keep one installation to one journal writer and one unfinished operation.
- Treat unsupported or ambiguous filesystem behavior as a closed failure.

## Final ruling

Accepted with constraints: the journal may become canonical, but it grants no
deployment authority.
