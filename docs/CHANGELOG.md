# Changelog

## Unreleased

### 2026-09-13 — Create the non-mutating stack plan kernel

Create the MIT-licensed `gotth-stack` repository, canonical product and safety
contracts, strict workflow decomposition, secret-free manifest model,
deterministic dependency planner, digest-bound output, and read-only CLI.

This slice does not deploy or inspect a host, load adapters, read secret values,
or call Docker, systemd, Caddy, Authentik, PostgreSQL, Mailu, DNS, GOTTH Mail,
or GOTTH Board. Exact source `baec0ebc089ff09ec2975ec23130aa9f35f60852`
passed the development-host gates recorded in the workflow evidence. Forgejo
PR #1 records final review and exact fast-forward admission.
