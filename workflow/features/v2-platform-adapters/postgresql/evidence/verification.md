# PostgreSQL runtime adapter verification evidence

Date: 2026-09-14

Candidate: `workflow/feature/postgresql-adapter`, based on admitted Caddy merge
`bb76093b93016fbcdf13557b37a60ee273e74ae4`. The exact admitted commit is
recorded by Git and the pull-request/merge record rather than self-referenced
inside the commit.

## Environment

- Development host: Linux amd64, Go 1.26.6.
- Docker client/server: client at `/usr/bin/docker`, fixed local Unix socket;
  server 29.7.1, API 1.55.
- Disposable images: PostgreSQL 17.10
  `postgres@sha256:a426e44bac0b759c95894d68e1a0ac03ecc20b619f498a91aae373bf06d8508d`
  and PostgreSQL 17.11
  `postgres@sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73`,
  both already present locally. No pull occurred.
- Full verification tree: private `/tmp/gotth-pg-verify.*` source copy with a
  fresh Git index and a private Node 26.7.0 binary; npm 12.0.2. The host
  installation was not changed.

## Required results

```text
sudo GOTTH_POSTGRES_DOCKER_BIN=/usr/bin/docker \
  GOTTH_POSTGRES_IMAGE=postgres@sha256:a426... \
  GOTTH_POSTGRES_REPLACEMENT_IMAGE=postgres@sha256:18cfe... \
  postgresql.test -test.run '^TestDisposablePostgreSQL17$' -test.v
PASS (20.69s)

go test -mod=readonly -race -count=100 ./internal/adapters/postgresql
PASS (27.591s)

go test -mod=readonly -race -shuffle=on -count=20 ./internal/adapters/postgresql
PASS (6.674s)

PATH=<private-node-26.7.0>:$PATH make verify
PASS
```

The disposable test proved fresh create/start/readiness, adapter reopen,
same-major digest replacement, previous-container preservation, candidate
verification, reverse stop/remove/restore/restart, exact prior-runtime
verification, and persistent `PG_VERSION` data. Cleanup inspection reported no
remaining proof container.

The full gate proved deterministic web generation, zero npm audit
vulnerabilities, formatting, source-boundary checks, vet, race tests, builds,
manifest validation, and planning. Package statement coverage was:

- `internal/adapters/postgresql`: 79.5%
- `internal/adapters/caddy`: 81.8%
- `pkg/stack`: 94.5%
- `pkg/journal`: 84.3%
- `internal/site`: 76.4%
- `cmd/gotth-stack`: 84.2%
- `cmd/gotthstack-web`: 56.0%

## Honest limits

Uncovered PostgreSQL statements are defensive direct-OS failures, cleanup
failures not injectible through the selected standard-library primitives,
fixed-struct serialization impossibilities, and redundant rejection branches.
The caller-controlled contract, runtime state transitions, restart points,
parsers, security drift, durable state ambiguity, and external command failure
paths are covered. No requirement exception is hidden by the percentage.

The adapter intentionally preserves database bytes rather than claiming to
roll them back. The proof used only private temporary data, secret, and state
roots and operation-derived disposable containers. It did not pull an image,
name or inspect a live container, change Docker policy, touch product data, or
grant the controller Docker or `apply` authority.
