# Public website

State is canonical in `workflow.toml`.

## Scope

Build the public `gotthstack.com` landing site as a separate Go executable with
templ, Tailwind CSS, HTMX progressive enhancement, embedded assets, strict
HTTP behavior, and no dependency on controller authority.

## Requirements

- `STACK-SITE-001` through `STACK-SITE-006` in `docs/prd.md`.

## Non-goals

- Deployment, DNS, Caddy, credentials, analytics, accounts, persistence,
  administrator controls, controller apply, tags, releases, pushes, or PRs.

## Evidence

Implementation evidence belongs in `evidence/verification.md`. Review belongs
in `review/`. These files support but do not override `workflow.toml`.
