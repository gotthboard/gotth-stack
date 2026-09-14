# Verification

## Plan-kernel gates

- format, vet, race, build, module-readonly, and clean-clone checks;
- 100% relevant coverage of accepted input, every validation class,
  duplicates, trailing values, size/depth bounds, graph failures,
  deterministic order/digests, input immutability, and CLI failure behavior;
- fuzz targets for strict parsing and graph planning;
- repeated and shuffled test runs;
- independent external-package consumer compile;
- source scan proving no apply/exec/network/Docker/systemd path;
- two cold review passes before admission.

## Approval and recovery journal gates

- exact plan, installation, actor-authority, expiry, component, artifact,
  configuration, capability, and secret-revision binding;
- exact duplicate idempotency and conflicting-ID rejection;
- strict installation/head/record parsing and fixed non-disclosing errors;
- log/head write, file sync, directory sync, truncate, rename, and close failure
  injection at every durable transition;
- subprocess lock-contention and crash/reopen tests;
- partial header and payload recovery only behind the durable head;
- checksum, chain, sequence, identity, schema, head-ahead, size, and transition
  corruption rejection with no partial trusted state;
- interrupted read-only retry versus interrupted mutation
  `recovery_required` behavior;
- cancellation, failure-point, rollback-reference, rollback failure, and
  terminal-state tests;
- frame/log limit-1, limit, limit+1, and materially-beyond boundary tests;
- race, repeat, shuffle, fuzz, coverage, clean-clone, source-boundary, and two
  cold review passes.

The completeness oracle is the exact validated record count, final sequence,
final digest, and durable head. A successful open without that agreement is
not evidence.

## Future mutation gates

Apply remains blocked until tests prove exact approval binding, journal fsync
and crash recovery, idempotent reconciliation, cancellation boundaries,
least-privilege adapter grants, secret redaction, backup-before-change,
rollback or honest recovery-required reporting, and disposable end-to-end
deployment for each supported topology.

No live host is a valid plan-kernel or journal test target.
