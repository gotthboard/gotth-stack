# Cold review pass 2 — authority, userspace, and cost

Reviewed journal code source:
`4a140590ed90472caad93d9d526fd4bbc1d89eb2`

Decision: **CLEAN**

This was a fresh admission pass focused on abuse resistance, permissions,
userspace behavior, and cost rather than repeating the transition review.

- The directory and four fixed files require exact private modes and
  effective-UID ownership. Fixed-file opens use `O_NOFOLLOW`, descriptors are
  revalidated, and a nonblocking lifetime `flock` prevents concurrent writers.
- Hostile JSON is bounded, depth-checked, duplicate-key-checked,
  unknown-field-checked, canonicalized, checksummed, chained, sequenced, and
  tied to one installation. Errors do not echo hostile input.
- Secret values, commands, environment, credentials, adapter payloads, and raw
  rollback material have no representation in the journal API or record
  schema.
- The CLI remains validate/plan only. Production source contains no network,
  shell, process execution, Docker, systemd, adapter loading, or apply path.
- Existing plan output and CLI behavior are unchanged. The new package is
  additive and therefore does not break current userspace or weaken approval
  semantics.
- The cost is explicit: one payload/frame construction and synchronous
  log/head/directory persistence per authority transition, bounded 4 MiB
  records, bounded 512 MiB replay, and one writer per installation. Avoiding
  these costs would make the durability claim dishonest.

The remaining statement-coverage gap is documented OS-error scaffolding and
unreachable defensive code, not an untested authority or parser path. No
unresolved security, permission, userspace, observability, or cost-model defect
remains in this workstream.
