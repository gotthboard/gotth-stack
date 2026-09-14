# Mail runtime topology correction review

## Decision

Accept.

## Finding

The original stack contract confused GOTTH Mail's historical Mailu reference
and import compatibility with its deployable runtime. That would have forced a
nonexistent Mailu adapter into V3 and hidden the real daemon boundaries.

## Corrected boundary

The stack now names the GOTTH Mail control plane, mail front/proxy, Postfix,
Dovecot, Rspamd, and PostgreSQL explicitly. Shared Caddy owns HTTP routes only;
the V3 contract must assign every listener, certificate, ACME path, and
hostname to one owner before apply exists. Authentik ordering does not make
mail delivery or daemon lookup depend on Authentik availability.

## Userspace and authority

No runtime userspace, permission, secret, product database, DNS record, or live
service changed. Mail and Board remain separate product adapters and failure
domains. The generic V0 planner is unchanged.
