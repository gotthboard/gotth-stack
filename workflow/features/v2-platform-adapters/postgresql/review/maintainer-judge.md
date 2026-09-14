# Maintainer review

Two cold passes were applied to the complete PostgreSQL slice.

## Findings corrected

- Container absence initially swallowed any failed `docker inspect` whenever
  the engine itself remained reachable. That was garbage recovery semantics.
  Exact filtered existence is now established separately; a present
  container whose inspection fails is an error, never absence.
- Transaction validation initially accepted any stored container-name string
  and ignored a simultaneous final transaction plus staging residue. Stored
  names are now re-derived, descriptor modes are constrained, and ambiguous
  durable siblings require recovery.
- The implementation specification described a transaction subdirectory that
  the mechanism did not create and omitted `ReconcileStage`. The document now
  names the actual canonical sibling-file protocol and complete API.
- Secret readability initially constrained UID/GID and mode but did not bound
  the owning UID. It now admits only root, the effective adapter user, or the
  declared container UID while still requiring actual container readability.
- Effective inspection initially checked required labels and environment but
  tolerated undeclared adapter-namespace labels and dangerous alternate
  `POSTGRES_*` initialization controls. Both are now rejected and covered as
  hostile drift.
- The first disposable proof covered only fresh installation. It now performs
  a real two-image PostgreSQL 17 replacement, reopen, reverse rollback, old
  runtime verification, and data-preservation proof.
- Local-only image inspection did not prevent `docker create` from pulling if
  that image disappeared in the check/use interval. Creation now fixes
  `--pull never`; a missing local image fails without opening registry access.
- The executable contract said root-owned while the first mechanism also
  admitted an effective-user-owned client. The implementation now requires
  root ownership; tests use the root-owned system executable seam.
- Transaction creation initially trusted the process umask to preserve owner
  read/write bits. New transaction descriptors now receive and verify exact
  `0600` mode before write, with a restrictive-umask regression test.

## Final ruling

Accepted as a private, unstable-alpha adapter candidate. Runtime mutation is
split into journalable calls, impossible states fail closed, rollback never
deletes database bytes, and the Docker-socket hazard is explicit. Admission is
strictly limited to this PostgreSQL child. Authentik, product topology, an
installed component-scoped privilege boundary, and controller `apply` remain
unfinished, so the parent workstream remains incomplete.
