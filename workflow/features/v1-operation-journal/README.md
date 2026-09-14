# Approval and recovery journal

The directory name and manifest ID preserve the historical `v1` workstream ID;
they are not a product version. Current product development remains on the
`1.0.0-alpha.N` line.

This workstream owns `STACK-JRN-001` through `STACK-JRN-012`: private stable
installation identity, exact approval binding, durable checksum/hash-chain
records, caller idempotency keys, replay, torn-tail recovery, corruption
refusal, cancellation limits, rollback references, and explicit
unknown-outcome state.

Mutation idempotency digests are operation-local and unique. Read-only retry
retains the exact step/component/phase identity. Failed mutation results are
rollback-eligible because failure does not prove that external state remained
untouched. An operation cannot hide possibly live mutation behind an ordinary
failed terminal; it must be rolled back or remain recovery-required.
Each approval authorizes exactly one operation ID, closing authorization replay
under a fresh operation name. Each installation admits only one unfinished
operation at a time.

It does not own an `apply` command, adapters, network access, secret retrieval,
Docker/systemd, a web UI, or live deployment. Completion means the journal can
prove what was approved, what transition was durably recorded, where failure
occurred, and whether restart may retry, must rollback, or requires operator
recovery. It does not mean any host mutation exists.

## Runtime boundary

- Linux 7.1 and Go 1.26.6 are the pinned evidence target.
- Local filesystem support must include regular-file/directory `fsync`, atomic
  same-directory replacement, and advisory `flock`.
- Unsupported filesystems fail closed.
- Network filesystems are outside this alpha contract.

## Required evidence

- requirements-to-test traceability for every `STACK-JRN-*` requirement;
- filesystem failure injection at every write/sync/rename/truncate boundary;
- restart, replay, duplicate, corruption, limit, lock, cancellation, rollback,
  and unknown-outcome tests;
- race, repeat, shuffle, fuzz, coverage, clean-clone, source-boundary, and two
  cold review passes;
- an explicit performance N/A or measured report. The journal intentionally
  pays one log sync plus one head-file sync/rename and one directory sync per
  authority transition; batching is forbidden because it would lie about
  durability.

## Completion

Journal code source `4a140590ed90472caad93d9d526fd4bbc1d89eb2`
passed the development-host gates recorded in `evidence/verification.md`.
The implementation satisfies `STACK-JRN-001` through `STACK-JRN-012` without
adding an adapter, executor, apply command, secret-value representation,
network client, deployment, tag, or release.
