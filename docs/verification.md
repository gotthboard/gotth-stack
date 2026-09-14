# Verification

## Plan-kernel gates

- format, vet, race, build, module-readonly, and clean-clone checks;
- 100% relevant coverage of accepted input, every validation class,
  duplicates, trailing values, size/depth bounds, graph failures,
  deterministic order/digests, input immutability, and CLI failure behavior;
- fuzz targets for strict parsing and graph planning;
- repeated and shuffled test runs;
- independent external-package consumer compile;
- source scan proving no apply/exec/network/Docker/systemd path in the plan
  kernel or controller CLI;
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

## Caddy adapter gates

- exact artifact and candidate SHA-256 binding;
- executable, parent, config, state, lock, transaction, symlink, hardlink,
  ownership, and mode refusal;
- literal-loopback Admin API and direct no-proxy HTTP client;
- fixed `adapt`, `validate`, and `reload` arguments with scrubbed environment,
  bounded output, context cancellation, and fixed errors;
- previous/candidate runtime synchronization before staging;
- durable staging and atomic config checkpoint failure injection;
- operation-ID conflict, duplicate idempotency, incomplete-stage
  reconciliation and rollback, unexpected-state refusal, and reopen recovery;
- exact file mode/UID/GID preservation independent of umask;
- disposable Caddy install, activation, unrelated-route survival, adapter
  reopen, restore, reactivation, and exact rollback verification;
- race, repeat, shuffle, coverage, full repository regression, source boundary,
  and clean-copy checks on the development host.

Only the disposable process is a mutation target. The gate never points the
adapter at `/etc/caddy`, systemd, DNS, or a live site.

## Public website gates

- reproducible templ, Tailwind CSS, and HTMX asset generation with pinned
  tools and no generated diff;
- Go formatting, vet, race, coverage, and build gates;
- exact route, method, content type, cache, browser-security, health, HTMX
  fragment, full-page fallback, canonical GitHub source-link, and hostile topic
  tests;
- source/import scan proving the public site has no controller, journal,
  adapter, process-exec, secret, tracking, cookie, or outbound-network path;
- external-process start, readiness, response, and graceful-termination smoke;
- narrow and wide headless-browser captures, semantic landmark inspection,
  keyboard-focus inspection, and a JavaScript-disabled full-page check;
- cold diff review before admission.

The site is performance-insensitive at current scale: it renders a fixed page
and scans three fixed principle records. Verification records response
allocations and benchmark distributions to catch accidental template or asset
regressions; no speedup claim is made.
