# Architecture

## Authority split

```text
operator/browser
      |
      v
gotth-stack controller
  plan / approval / journal / audit / recovery
      |
      +--> Caddy adapter
      +--> Authentik adapter
      +--> PostgreSQL adapter
      +--> GOTTH Mail adapter
      +--> mail front/proxy adapter
      +--> Postfix adapter
      +--> Dovecot adapter
      +--> Rspamd adapter
      +--> GOTTH Board adapter
      +--> admitted gotth-extension-* provider
```

The controller is the only composition authority. Adapters receive only the
component-specific target and capability grant needed for one operation. They
do not receive another component's secrets, administrator session, database,
or rollback state.

V0 implements only the upper-left planning boundary. It parses a secret-free
manifest, normalizes unordered declarations, validates the dependency graph,
and emits a deterministic plan. It does not load adapters or inspect a host.

## Manifest and plan identity

The manifest binds each component to:

- one stable component ID;
- one versioned adapter identity;
- one artifact source label and exact SHA-256 content digest;
- one exact configuration digest;
- explicit dependencies;
- explicit requested capabilities; and
- named secret slots without values.

The normalized manifest sorts components and every set-like field. Its SHA-256
digest is the desired-state identity. The planner performs a stable
topological sort and hashes the resulting plan without its own digest field.
The human-readable source label is inert in V0; a future adapter admission
layer must validate and retrieve it without treating text as a command.
The digests detect inconsistent or changed content; they are not signatures
and do not establish artifact publisher authenticity.

## Failure model

- Invalid or ambiguous input produces no plan.
- A dependency cycle produces no partial order.
- Planner output is constructed in memory and written only after success.
- Rejection errors use fixed reason codes/messages and never interpolate input
  values. Successful plans intentionally display the accepted non-secret
  component, adapter, artifact-source, capability, and secret-slot identifiers
  that the operator is being asked to approve.
- Repeated validation or planning is side-effect free and byte-identical.

Future apply cannot simply call adapters in a loop. It requires a durable
journal written before each transition, an exact approval binding, adapter
preflight and recovery contracts, and an explicit unknown-outcome state. If
rollback cannot be proven, the UI must say so instead of fabricating a green
button.

## Integration boundaries

`gotth-infrastructure`, `gotth-authentik`, `gotth-release`, and
`gotth-pg-migrate` remain independently versioned mechanism libraries. Future
adapters may pin them exactly; `gotth-stack` does not absorb or duplicate their
policy. `gotth-extensions` defines extension compatibility, while a concrete
`gotth-extension-*` repository supplies one provider mechanism. Neither grants
deployment authority by naming convention.

## Mail runtime topology

Mailu is not deployed by `gotth-stack`. GOTTH Mail owns the mail control plane
and canonical domain/mailbox/alias state. Separate adapters manage its
front/proxy, Postfix, Dovecot, and Rspamd processes without turning those core
daemon contracts into provider extensions.

Shared Caddy may serve the GOTTH Mail administrator and custom webmail HTTP
routes alongside other domains. It does not replace the mail-specific ingress
or proxy ordinary SMTP/IMAP traffic. Before V3 can apply anything, its topology
contract must assign each public port, certificate, ACME challenge, and
hostname to exactly one owner; Caddy and mail front/proxy may not both claim the
same listener or certificate lifecycle.

The deployment graph may order Authentik configuration before GOTTH Mail
identity wiring, but that ordering does not create a runtime availability
dependency. Postfix delivery, Dovecot access/lookup, and Rspamd decisions must
continue when Authentik is unavailable. A shared mail runtime may host multiple
domains, with per-domain DNS, DKIM, TLS, policy, alias, and mailbox state.
