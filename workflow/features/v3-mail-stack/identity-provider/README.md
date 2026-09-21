# Identity, certificate, and DNS composition

This child composes exact desired state for GOTTH Mail, Authentik, Caddy, the
mail front, and one independently admitted DNS provider. It does not apply
that state.

Contracts:

- [product requirements](prd.md)
- [architecture](architecture.md)
- [implementation specification](implementation-spec.md)

All three source prerequisites are satisfied: GOTTH Mail's durable role-binding
operator at `c3d40453b4010026263ea1749b82812a52d03596`, Authentik's file-backed
OIDC/SCIM renderer at `7f7f86e21fbb7ca85e3a0ba78e381509326d6d03`, and GoDaddy SRV mutation at
`8529e8d13dcd7634c6810b1b937fb652564283a4`. Candidate status does not imply
public provider publication or live deployment.

The local composition remains intentionally non-production-ready. It names
provider publication, PTR authority, and the missing admitted Authentik
blueprint-application mechanism instead of converting them into operator
folklore or caller-supplied booleans.
