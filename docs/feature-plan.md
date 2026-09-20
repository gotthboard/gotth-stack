# Feature plan

Historical workflow IDs use `v0` through `v6`; they predate the release-stage
rule and are not product versions. Current work is reported by descriptive
workstream name. GOTTH Stack remains `1.0.0-alpha.N` until all required
features are complete.

1. Plan kernel (historical ID `v0.plan-kernel`) — strict secret-free manifest, deterministic dependency
   plan, digest binding, read-only CLI, hostile tests, and evidence.
2. Approval and recovery journal (historical ID `v1.operation-journal`) —
   durable installation identity, exact approval binding, fsync-backed
   checksum/hash-chain journal, replay, cancellation limits, rollback
   references, and unknown-outcome recovery. The journal is implemented and
   verified for the current `1.0.0-alpha.N` line; it grants no deployment
   authority.
3. Public website (`public-site`) — a separate informational
   `gotthstack-web` executable using Go, templ, Tailwind CSS, and HTMX. It has
   no controller imports or mutation authority and can proceed independently
   while the platform-adapter workstream remains ready.
4. Platform adapters (historical ID `v2.platform-adapters`) — split into
   separately admitted Caddy, Authentik, and PostgreSQL adapters. The Caddy
   child owns only complete-config preflight, durable staging, atomic file
   replacement, runtime activation/observation, rollback, and disposable
   proof. The PostgreSQL child owns one digest-pinned PostgreSQL 17 container,
   durable runtime-replacement state, exact effective inspection, readiness,
   observation, data-preserving rollback, and disposable proof. The Authentik
   child owns one hardened server/worker pair with file-referenced
   secrets, exact observation, health, recovery, and persistent-state-
   preserving rollback. Completing any individual child does not complete the
   parent workstream or unblock an apply command by itself.
5. Mail stack (historical ID `v3.mail-stack`) — decomposed into a frozen
   topology/release/backup contract, five typed role adapters, journal-bound
   controller execution, identity/certificate/DNS composition, and complete
   disposable acceptance. Mailu and the reference Compose fixtures are not
   runtime components.
6. Board stack (historical ID `v4.board-stack`) — GOTTH Board adapter, isolated Authentik/PostgreSQL/Caddy
   composition, migration/readiness proof, backup, and rollback.
7. Administrator UI (historical ID `v5.admin-ui`) — first-run and operations UI rendered by the controller with
   Go, templ, Tailwind, HTMX, and ordinary HTML fallback.
8. Provider extensions (historical ID `v6.provider-extensions`) — admit concrete `gotth-extension-*` providers one
   repository at a time; start with DNS only after its capability contract is
   smaller than generic host authority.

Only one feature is active at a time. A later feature may split further before
implementation. No step may use the presence of a planned successor as proof
that its safety boundary exists.
