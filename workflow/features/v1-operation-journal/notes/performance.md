# Performance and cost model

This workstream has no deployment throughput claim. A journal transition is an
authority boundary, not a bulk-data path, so batching is forbidden.

- Each successful append marshals one canonical JSON payload, builds one framed
  copy, writes and syncs the log, writes and syncs a replacement head, renames
  it, and syncs the directory.
- The 4 MiB payload ceiling exists for the largest valid approval projection.
  Such an approval temporarily holds both payload and frame copies; ordinary
  step records are small.
- Open scans at most 512 MiB and 262,144 frames linearly. It retains immutable
  approvals and operation/step summaries in memory. This is deliberately
  bounded but not cheap.
- `Approval` is a map lookup plus deep copy. `ActiveOperation` performs an
  allocation-free scan of operation summaries and deep-copies at most one
  result. History pagination and indexing are deferred until measured use
  justifies another format.
- Every public mutation is serialized by one mutex and one installation-wide
  advisory file lock held for the journal lifetime. Parallel journal writes are
  intentionally unsupported.

A synthetic operations-per-second benchmark would be dishonest because the
dominant cost is the target filesystem's flush behavior. Verification instead
runs the exact sync path on development's ZFS dataset and uses tmpfs only for
high-count race/repeat/fuzz checks. No performance exception is requested.
