# gotth-stack

`gotth-stack` is the bootstrap and deployment controller for GOTTH products.
It will coordinate Caddy, Authentik, PostgreSQL, GOTTH Mail's front/proxy,
Postfix, Dovecot, Rspamd, and GOTTH Board through separate, least-privilege
adapters while keeping provider-specific mechanisms in independent
`gotth-extension-*` repositories.

The admitted plan kernel (historical workflow ID `v0.plan-kernel`) is
deliberately non-mutating. It validates a strict,
secret-free stack manifest and renders a deterministic, digest-bound deployment
plan:

```sh
gotth-stack validate stack.json
gotth-stack plan stack.json
```

The active approval-and-recovery journal workstream records durable authority
and unknown-outcome state but still executes nothing. There is no `apply`
command. External mutation remains blocked until the journal, adapter rollback
contracts, and disposable recovery tests are admitted. A pretty plan or journal
record is not permission to touch a host.

GOTTH Stack uses the product release stages `1.0.0-alpha.N`,
`1.0.0-beta.N`, `1.0.0-rc.N`, and `1.0.0`. Historical `v0` through `v6`
workflow IDs name internal workstreams, not product versions.

## Boundaries

- Stack manifests contain artifact/configuration digests and named secret
  slots, never secret values.
- Component dependencies are explicit and cycle-free.
- Plans are deterministic across input ordering and bound by SHA-256.
- Adapters remain separate: shared orchestration does not become a universal
  component full of product conditionals.
- DNS and other provider mechanisms remain `gotth-extension-*` services.
- No library or CLI operation currently invokes Docker, systemd, Caddy,
  Authentik, PostgreSQL, a mail daemon, a DNS API, or a live GOTTH product.
- Mailu is not a runtime component. GOTTH Mail retains only its separately
  documented reference/import compatibility; `gotth-stack` does not deploy it.

See `docs/` for the product, architecture, implementation, verification, and
feature contracts.

`examples/full-stack.json` demonstrates dependency composition for Caddy,
Authentik, PostgreSQL, the GOTTH Mail control plane and mail daemons, and GOTTH
Board. Its URLs and digests are inert placeholders and cannot be deployed.
