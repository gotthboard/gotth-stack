# gotth-stack

`gotth-stack` is the bootstrap and deployment controller for GOTTH products.
It will coordinate Caddy, Authentik, PostgreSQL, Mailu, GOTTH Mail, and GOTTH
Board through separate, least-privilege adapters while keeping provider-specific
mechanisms in independent `gotth-extension-*` repositories.

The admitted V0 slice is deliberately non-mutating. It validates a strict,
secret-free stack manifest and renders a deterministic, digest-bound deployment
plan:

```sh
gotth-stack validate stack.json
gotth-stack plan stack.json
```

There is no `apply` command yet. External mutation remains blocked until the
durable journal, confirmation binding, adapter rollback contract, and recovery
tests are admitted. A pretty plan is not permission to touch a host.

## Boundaries

- Stack manifests contain artifact/configuration digests and named secret
  slots, never secret values.
- Component dependencies are explicit and cycle-free.
- Plans are deterministic across input ordering and bound by SHA-256.
- Adapters remain separate: shared orchestration does not become a universal
  component full of product conditionals.
- DNS and other provider mechanisms remain `gotth-extension-*` services.
- No library or CLI operation currently invokes Docker, systemd, Caddy,
  Authentik, PostgreSQL, Mailu, a DNS API, or a live GOTTH product.

See `docs/` for the product, architecture, implementation, verification, and
feature contracts.

`examples/full-stack.json` demonstrates dependency composition for Caddy,
Authentik, Mailu, GOTTH Mail, and GOTTH Board. Its URLs and digests are inert
placeholders and cannot be deployed.
