# Changelog

## Unreleased

### 2026-09-13 — Create the non-mutating stack plan kernel

Create the MIT-licensed `gotth-stack` repository, canonical product and safety
contracts, strict workflow decomposition, secret-free manifest model,
deterministic dependency planner, digest-bound output, and read-only CLI.

This slice does not deploy or inspect a host, load adapters, read secret values,
or call Docker, systemd, Caddy, Authentik, PostgreSQL, Mailu, DNS, GOTTH Mail,
or GOTTH Board. Runtime verification and exact admitted commit are recorded in
the workflow evidence before this entry is considered complete.
