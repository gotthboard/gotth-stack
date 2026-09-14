# Maintainer review

Two cold passes were applied to the complete Authentik slice.

## Findings corrected

- The first host-network topology gave the worker the server's HTTP and HTTPS
  listener ports. A real Authentik pair immediately exposed the collision.
  The specification now binds six distinct role-scoped Authentik listeners,
  and exact effective inspection reconstructs each role separately.
- Persistent directory identity initially bound directory size and required a
  link count of two. That was garbage: valid runtime-created children change
  both. The transaction now binds device, inode, ownership, and mode while a
  regression test proves content growth does not invalidate the root.
- The derived component parent was not itself checked, leaving a symlink and
  rename seam ahead of Docker's path-based bind. It is now an exact private,
  adapter-owned, non-symlink directory; the runtime UID owns only its children.
- Corrupt transaction state was initially collapsed into the same error as an
  absent operation in mutation and verification paths. Corruption now remains
  `ErrUnsafeState`; only genuine absence maps to recovery-required.
- Preflight initially tolerated a running worker with a stopped server. That
  incoherent dependency state now fails closed and is covered directly.
- Preserve and restore accepted only their predecessor, not the immediate
  successful successor needed after an unknown Docker rename result. Both role
  paths are now idempotent at that boundary and tested twice per transition.
- The Authentik signing key was only bounded as a generic secret. It now
  enforces the documented minimum of 50 bytes without persisting or exposing
  the value.
- The first clean-copy gate lacked a Git index, making its formatting command
  vacuous despite the rest of the suite passing. Final verification uses a
  private committed snapshot so tracked-file checks exercise the exact tree.

## Final ruling

Accepted as a private, unstable-alpha adapter candidate. The server/worker
ordering is explicit, each external mutation is separately journalable,
secret bytes remain in fixed read-only files, effective Docker state is exact,
ambiguous recovery fails closed, and reverse rollback preserves persistent
state. Admission closes only the platform-adapter workstream. No installed
component-scoped Docker grant or journal-driven controller `apply` path exists,
so the product remains unable to deploy a host.
