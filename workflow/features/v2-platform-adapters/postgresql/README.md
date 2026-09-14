# PostgreSQL runtime adapter

Completed, locally verified critical child implementing `STACK-PG-001` through
`STACK-PG-011` for one
digest-pinned PostgreSQL 17 container. It owns fixed local-engine inspection,
durable transaction staging, explicit runtime replacement steps, exact
effective-state and readiness verification, restart observation, and reverse
runtime rollback that never deletes database bytes.

It does not own Docker installation or policy, image pulling, secret creation
or rotation, SQL migrations, logical or physical backups, data deletion,
major-version upgrades, product topology, Authentik, or controller `apply`.
The controller CLI must remain structurally unable to invoke this package.

Evidence and review records are under this folder. `workflow.toml` remains the
canonical state. Completion of this child does not complete the parent
platform-adapter workstream or create controller authority.
