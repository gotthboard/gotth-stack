# Caddy adapter verification evidence

Date: 2026-09-14

Candidate: `workflow/feature/caddy-adapter`, based on
`a2b82ea897f6f5df4a3d53534ac360d37cbac117`. The exact admitted commit is
recorded by Git and the pull-request/merge record; it is intentionally not
self-referenced here because doing so would change the object being named.

## Environment

- Development host: Linux amd64, Go 1.26.6 selected by the module toolchain.
- Disposable runtime: Caddy v2.11.4 at `/usr/bin/caddy`.
- Web generation: Node v26.7.0 copied into the disposable verification tree;
  npm 12.0.2. The development host installation was not modified.
- Full verification tree: a fresh `/tmp/gotth-stack-caddy-final.*` copy with a
  newly initialized Git index, excluding `.git` and `node_modules` from the
  source transfer.

## Required results

```text
GOTTH_CADDY_BIN=/usr/bin/caddy go test -race -run '^TestDisposableCaddy$' -v ./internal/adapters/caddy
PASS: disposable install, activate, reopen, observe, restore, reactivate,
exact prior-state verification, and unrelated-route survival

go test -race -count=100 ./internal/adapters/caddy
PASS (50.347s)

go test -race -shuffle=on -count=20 ./internal/adapters/caddy
PASS (10.742s)

PATH=<private-node-26.7.0>:$PATH GOTTH_CADDY_BIN=/usr/bin/caddy make verify
PASS
```

The complete gate proved deterministic templ/Tailwind/HTMX generation, npm
audit with zero vulnerabilities, Go formatting, source-boundary scans, vet,
race tests, builds, manifest validation, and planning. Package statement
coverage in that gate was:

- `internal/adapters/caddy`: 81.8%
- `pkg/stack`: 94.5%
- `pkg/journal`: 84.3%
- `internal/site`: 76.4%
- `cmd/gotth-stack`: 84.2%
- `cmd/gotthstack-web`: 56.0%

The Caddy package tests cover every adapter-visible state combination,
transaction and persistent-file durability checkpoint, caller-controlled
validation class, cancellation path, fixed command boundary, runtime response
class, hostile-value disclosure boundary, lock/reopen path, symlink/hardlink
refusal, exact UID/GID/mode preservation under umask, stale temporary-file
recovery, and real Caddy rollback sequence.

## Honest limits

The uncovered Caddy statements are defensive direct-OS failures, cleanup
failures that cannot be injected through the selected standard-library
mechanism, and fixed-struct serialization failures. Every requirement surface
and practical externally controlled failure path is covered; no coverage
exception weakens a required behavior.

Filesystem tests prove the requested Linux write, file-sync, rename, and
directory-sync ordering. They cannot prove storage hardware honors a reported
successful flush. The disposable test mutates only private temporary files and
processes. It never names `/etc/caddy`, invokes systemd, changes DNS, reloads a
live service, or gives the controller an `apply` command.
