# Authentik runtime adapter

Completed critical child for `STACK-AUTH-001` through `STACK-AUTH-012`. It owns
one digest-pinned Authentik 2026.5 server/worker pair, file-referenced secret
bindings, durable replacement state, exact effective inspection, readiness,
crash observation, and data-preserving reverse rollback.

It does not own PostgreSQL, Docker policy, image pulls, secret creation,
tenant/application configuration, outposts, backups, live services, or the
controller `apply` command. Evidence and review records live below this folder;
`workflow.toml` remains canonical.
