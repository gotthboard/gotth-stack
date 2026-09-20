# Mail stack contract verification — 2026-09-20

## Candidate

Contract implementation commit: `fc54b45bc0dd7616ab429b2b6e29ff5f7988f1f7`.

Platform-adapter prerequisite: canonical `main`
`fc86ba250a6690e8e2f0f392c4be7ef9b233fb9d`.

## Verification

Development host: `10.0.0.97`.

The exact repository pin requires Go 1.26.6, Node 26.7.0, and npm 12.0.2. The
host's system Node was 26.5.1, so the gate used a temporary Node 26.7.0 archive
whose SHA-256 matched Node's published checksum, then installed npm 12.0.2
inside that temporary toolchain. The repository pins were not weakened.

`make verify` passed, including deterministic templ/Tailwind/HTMX generation,
source-boundary checks, vet, race tests, coverage, all command builds, and
plan-kernel validation. Recorded package statement coverage was:

- `pkg/stack`: 94.5%;
- `pkg/journal`: 84.3%;
- Caddy adapter: 81.8%;
- PostgreSQL adapter: 79.4%;
- Authentik adapter: 72.8%;
- public site: 76.4%;
- controller CLI: 84.2%.

`workflow.toml` parsed with `v3.mail-stack.contract` as the sole active child
during the candidate run. `git diff --check`, example validation, and
deterministic plan generation passed. Two cold reviews are recorded beside
this evidence.

No adapter, controller apply path, credential, hostname, DNS/PTR record,
container, release, deployment, or live host was changed by this child.
