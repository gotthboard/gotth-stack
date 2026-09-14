# Authentik runtime adapter verification evidence

Date: 2026-09-14

Candidate: `workflow/feature/authentik-adapter`, based on admitted PostgreSQL
merge `b5dd28ad22fbc35b0d9cd681f7f14dd97554ca14`. The exact admitted commit is
recorded by Git and the pull-request/merge record rather than self-referenced
inside the commit.

## Environment

- Development host: Linux amd64, Go 1.26.6.
- Docker client/server: client at `/usr/bin/docker`, fixed local Unix socket;
  server 29.7.1, API 1.55.
- Disposable Authentik image: 2026.5.2 at
  `ghcr.io/goauthentik/server@sha256:3ddf09bbf69ded6a9634ecd753a01608d477f811e99bb5ffe9fc2ef7ad1c6581`.
- Disposable PostgreSQL fixture: PostgreSQL 17 at
  `postgres@sha256:a426e44bac0b759c95894d68e1a0ac03ecc20b619f498a91aae373bf06d8508d`.
  Both images were already present locally; no pull occurred.
- Full verification tree: private `/tmp/gotth-authentik-verify.*` source copy
  with a fresh Git snapshot and a private Node 26.7.0 binary; npm 12.0.2. The
  development host installation was not changed.

## Required results

```text
sudo GOTTH_AUTHENTIK_DOCKER_BIN=/usr/bin/docker \
  GOTTH_AUTHENTIK_IMAGE=ghcr.io/goauthentik/server@sha256:3ddf... \
  authentik.test -test.run '^TestDisposableAuthentik2026_5$' -test.v
PASS

go test -mod=readonly -race -count=100 ./internal/adapters/authentik
PASS

go test -mod=readonly -race -shuffle=on -count=20 ./internal/adapters/authentik
PASS

PATH=<private-node-26.7.0>:$PATH make verify
PASS
```

The disposable test proved fresh server/worker creation, server readiness
before worker start, both role health checks, adapter reopen, managed
configuration replacement, prior-pair preservation, candidate verification,
reverse stop/remove/restore/restart, exact prior-pair verification, and
persistent certificate-state survival. Cleanup inspection reported no proof
container or PostgreSQL fixture remaining.

The full gate proved deterministic web generation, zero npm audit
vulnerabilities, formatting, source-boundary checks, vet, race tests, builds,
manifest validation, and planning. Package statement coverage was:

- `internal/adapters/authentik`: 72.8%
- `internal/adapters/postgresql`: 79.4%
- `internal/adapters/caddy`: 81.8%
- `pkg/stack`: 94.5%
- `pkg/journal`: 84.3%
- `internal/site`: 76.4%
- `cmd/gotth-stack`: 84.2%
- `cmd/gotthstack-web`: 56.0%

## Honest limits

Uncovered Authentik statements are defensive direct-OS failures, fixed-struct
serialization impossibilities, subprocess launch failures covered at the
bounded runner seam, and redundant rejection branches. Caller-controlled
input, secret and directory bindings, effective container shape, every
forward/reverse role operation, immediate idempotent successors, corruption,
unknown outcomes, reopen, health, and the real runtime path are covered. No
requirement exception is hidden by the percentage.

The proof used only private temporary state, data, secret, and container names.
It did not pull an image, mount the Docker socket into Authentik, name or
inspect a live container, change Docker policy, touch product data, configure
an Authentik tenant/outpost, or grant the controller Docker or `apply`
authority.
