# Feature plan

1. `v0.plan-kernel` — strict secret-free manifest, deterministic dependency
   plan, digest binding, read-only CLI, hostile tests, and evidence.
2. `v1.operation-journal` — durable installation identity, approval token,
   fsync-backed journal, replay, cancellation, and unknown-outcome recovery.
3. `v2.platform-adapters` — separate Caddy, Authentik, and PostgreSQL adapter
   contracts and disposable integration proofs.
4. `v3.mail-stack` — Mailu and GOTTH Mail adapters, OIDC/SCIM composition,
   DNS/certificate provider seam, mail-flow health, backup, and rollback.
5. `v4.board-stack` — GOTTH Board adapter, isolated Authentik/PostgreSQL/Caddy
   composition, migration/readiness proof, backup, and rollback.
6. `v5.admin-ui` — first-run and operations UI rendered by the controller with
   Go, templ, Tailwind, HTMX, and ordinary HTML fallback.
7. `v6.provider-extensions` — admit concrete `gotth-extension-*` providers one
   repository at a time; start with DNS only after its capability contract is
   smaller than generic host authority.

Only one feature is active at a time. A later feature may split further before
implementation. No step may use the presence of a planned successor as proof
that its safety boundary exists.
