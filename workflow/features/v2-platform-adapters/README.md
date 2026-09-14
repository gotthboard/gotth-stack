# V2 platform adapters

The original item was too broad for one reviewable critical feature. It is
split into separately admitted Caddy, Authentik, and PostgreSQL children.

The admitted Caddy, PostgreSQL, and Authentik children are documented below
this folder. Their private runtime mechanisms have separate disposable
replacement and rollback evidence. This completes the platform-adapter
workstream, but it does not create an execution boundary or controller `apply`
command. Those remain separate critical work.
