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

The approval-and-recovery journal records durable authority, step intent,
results, rollback compensation, and unknown-outcome state but executes
nothing. The private platform mechanisms are separate Caddy, PostgreSQL 17,
and Authentik 2026.5 adapters with digest-bound preflight, durable replacement
state, exact runtime observation, and disposable rollback proofs. No CLI calls
them and there is no `apply` command. External controller mutation remains
blocked until a least-privilege journal-driven execution path is separately
admitted. A pretty plan, journal record, or private adapter package is not
permission to touch a host.

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
- No `gotth-stack` CLI operation currently invokes an adapter, Docker,
  systemd, Caddy, Authentik, PostgreSQL, a mail daemon, a DNS API, or a live
  GOTTH product. Private adapters expose only component-scoped, separately
  journalable mechanisms; they do not grant the controller Docker, Caddy, or
  product authority. The separate public website only serves informational
  HTTP responses.
- Mailu is not a runtime component. GOTTH Mail retains only its separately
  documented reference/import compatibility; `gotth-stack` does not deploy it.

See `docs/` for the product, architecture, implementation, verification, and
feature contracts.

## Public website

`gotthstack-web` serves the public `gotthstack.com` landing site with Go,
templ, Tailwind CSS, and HTMX. It is deliberately separate from controller
authority: it imports neither `pkg/stack` nor `pkg/journal`, exposes no
administration or apply operation, and makes the complete page usable without
JavaScript.

```sh
make generate-web
go run ./cmd/gotthstack-web
```

`examples/full-stack.json` demonstrates dependency composition for Caddy,
Authentik, PostgreSQL, the GOTTH Mail control plane and mail daemons, and GOTTH
Board. Its URLs and digests are inert placeholders and cannot be deployed.
