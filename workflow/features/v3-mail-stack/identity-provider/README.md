# Identity, certificate, and DNS composition

This child composes exact desired state for GOTTH Mail, Authentik, Caddy, the
mail front, and one independently admitted DNS provider. It does not apply
that state.

Contracts:

- [product requirements](prd.md)
- [architecture](architecture.md)
- [implementation specification](implementation-spec.md)

Implementation is blocked until GOTTH Mail has a reviewed durable role-binding
operator path and the GoDaddy extension artifact admits required SRV mutation.
The Authentik file-backed OIDC/SCIM renderer is already satisfied and mirrored
at `7f7f86e21fbb7ca85e3a0ba78e381509326d6d03`.
