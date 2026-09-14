# Verification

## V0 gates

- format, vet, race, build, module-readonly, and clean-clone checks;
- 100% relevant coverage of accepted input, every validation class,
  duplicates, trailing values, size/depth bounds, graph failures,
  deterministic order/digests, input immutability, and CLI failure behavior;
- fuzz targets for strict parsing and graph planning;
- repeated and shuffled test runs;
- independent external-package consumer compile;
- source scan proving no apply/exec/network/Docker/systemd path;
- two cold review passes before admission.

## Future mutation gates

Apply remains blocked until tests prove exact approval binding, journal fsync
and crash recovery, idempotent reconciliation, cancellation boundaries,
least-privilege adapter grants, secret redaction, backup-before-change,
rollback or honest recovery-required reporting, and disposable end-to-end
deployment for each supported topology.

No live host is a valid V0 test target.
