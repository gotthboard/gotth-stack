# Identity, certificate, and DNS composition

This child composes exact desired state for GOTTH Mail, Authentik, Caddy, the
mail front, and one independently admitted DNS provider. It does not apply
that state.

Contracts:

- [product requirements](prd.md)
- [architecture](architecture.md)
- [implementation specification](implementation-spec.md)

Implementation is blocked until the GoDaddy extension artifact admits required
SRV mutation. GOTTH Mail's reviewed durable role-binding operator is satisfied
and mirrored at `c3d40453b4010026263ea1749b82812a52d03596`; the Authentik
file-backed OIDC/SCIM renderer is satisfied and mirrored at
`7f7f86e21fbb7ca85e3a0ba78e381509326d6d03`.
