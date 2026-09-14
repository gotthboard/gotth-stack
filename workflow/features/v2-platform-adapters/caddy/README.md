# Caddy adapter

Completed, locally verified critical feature implementing `STACK-CADDY-001`
through `STACK-CADDY-010`.

The slice owns a fixed-binary, secret-free complete-Caddyfile adapter with
strong preflight, private durable transaction state, atomic persistence,
loopback Admin API activation and observation, exact recovery classification,
and reverse-order rollback. It owns disposable Caddy evidence but no live
deployment, systemd integration, product route composition, secret retrieval,
controller apply command, Authentik work, or PostgreSQL work.

Admission requires hostile unit coverage, every durability checkpoint,
restart reconciliation, a real disposable Caddy install/rollback proof, full
development-host race/repeat/coverage/build gates, and explicit remaining-gap
evidence.

The verification and cold-review records are under `evidence/` and `review/`.
The candidate remains uncommitted and unpublished; completion of this child
does not admit the parent workstream or create controller apply authority.
