# Approval and recovery journal verification

Verified source: `4a140590ed90472caad93d9d526fd4bbc1d89eb2`

## Evidence environment

- Host: `development` (`10.0.0.97`), not the gateway host.
- Toolchain: `go version go1.26.6 linux/amd64`.
- Durability filesystem: ZFS at `/tank`, mounted
  `rw,noatime,xattr,posixacl,casesensitive`.
- Repeat/fuzz scratch: tmpfs at `/tmp`; this evidence is not used to claim
  power-loss durability.
- Repository input: clean single-branch Forgejo clone of the exact source hash.

## Required gates

The exact source passed `make verify` with `TMPDIR` on ZFS. That gate ran:

- pinned Go version check;
- gofmt cleanliness;
- `go vet -mod=readonly ./...`;
- `go test -mod=readonly -race -cover ./...`;
- `go build -mod=readonly ./cmd/gotth-stack`;
- example manifest validation and deterministic plan rendering.

Package results from that ZFS-backed race run:

- `cmd/gotth-stack`: pass, 84.2% statements;
- `pkg/journal`: pass, 84.3% statements;
- `pkg/stack`: pass, 94.5% statements.

The same exact source then passed on development tmpfs:

- `go test -mod=readonly -race -count=100 ./pkg/journal` — pass in
  487.977 seconds;
- `go test -mod=readonly -shuffle=on -count=20 ./pkg/journal` — pass in
  9.110 seconds;
- `go test -mod=readonly -run '^$' -fuzz '^FuzzStrictJSON$' -fuzztime=30s
  ./pkg/journal` — pass, 3,967,746 executions and 336 new interesting inputs;
- `go test -mod=readonly -run '^$' -fuzz '^FuzzFrameShape$' -fuzztime=30s
  ./pkg/journal` — pass, 13,957,388 executions and 27 new interesting inputs;
- focused coverage rerun — 84.3% statements;
- TOML and every JSONL event parsed successfully;
- final clone status was clean.

The source-boundary gate found no production import or use of `net/http`,
`os/exec`, process execution syscalls, Docker, systemd, an apply function, or an
`apply` CLI command. `pkg/journal` depends only on the Go standard library and
the local `pkg/stack` plan contract.

## Requirement behavior

The suite proves:

- exact private modes, effective-UID ownership, `O_NOFOLLOW`, directory and
  descriptor validation, installation identity, and cross-process locking;
- largest-valid plan approval projection, exact secret revision coverage,
  single-operation approval authority, expiry, duplicate stability, and
  conflicting reuse refusal;
- one unfinished operation per installation and restart discovery through
  `ActiveOperation` plus immutable approval lookup;
- write-before-effect framing and injected failure at log write/sync, head
  write/sync/rename, directory sync, truncation, truncation sync, and close;
- strict/canonical JSON, checksum, sequence, hash-chain, schema, identity,
  envelope/body time, state-machine, head, record, frame, and log bounds;
- partial header/payload truncation only behind the durable head, one-record
  stale-head repair, and refusal of committed corruption or an ahead head;
- exact read-only restart retry, mutation reconciliation, unknown outcome,
  failure point, cancellation boundary, operator rollback, failed-mutation
  compensation, recovery-only refusal, and terminal-state rules;
- no ordinary terminal failure after mutation may have left external state
  live; the result must be rolled back or remain recovery-required;
- fixed non-disclosing errors and deep-copy public results.

## Coverage gap

The journal issue surface does not reach 100% statement coverage. The remaining
15.7% is primarily direct OS failure branches that cannot be induced safely
through ordinary files (`Mkdir`, `OpenRoot`, initial file `Sync`/`Close`, lock
unlock, root close), impossible marshal failures for fixed structs, and
defensive internal branches that public validation makes unreachable. The
storage operation seams inject every durability checkpoint whose ordering can
change recovery behavior, and hostile replay/fuzz tests cover every
attacker-controlled parser path practical to drive. No uncovered product
requirement or mutation-authority branch is being waived.

Go and Linux tests cannot prove that storage hardware honors a successful
flush after power loss. The evidence proves GOTTH Stack issues and checks the
documented file and directory synchronization calls in order on ZFS. Hardware
fault-injection remains a later release gate, not a hidden claim here.

## Code graph and context evidence

- Context broker version `0.1.0`; packet SHA-256
  `8826786c571a5b906b23bda74e91dcbcf055a592b315e3512564b7c6da9924bb`;
  cache miss; packet was not budget-truncated. Its repository
  scan reported 36 matches and returned the configured 12-file/100-line cap.
- Graphify version `0.9.32`, code-only extraction for the verified source:
  293 nodes, 1,002 edges; graph SHA-256
  `8a114cc084ff439b0d1ab0eef75d96edbee81acc350756d9a5b7bf6da5231157`.
- Graphify skipped documentation/unclassified files as configured and warned
  that the JSON example produced no code node. That does not affect Go impact
  analysis.

The journal has no benchmark admission requirement. Its explicit allocation,
sync, replay, and locking costs are recorded in `../notes/performance.md`.
