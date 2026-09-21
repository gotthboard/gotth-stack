# Mail identity/provider judge loop — 2026-09-21

## Ruling

Accepted locally at `79a1547d812cb3739dd24b0d6bdff262c92b9612`.
Fresh cold pass 9 was CLEAN and independent merge-authority pass 10 was
CLEAN/ACCEPT on identical tree
`ce5737b7ebb289ec9641d44283ecad1414f51bac`.

## Reject/fix history

1. Rejected implicit Caddy HTTP/3/UDP, mutable unsigned results, readiness
   without PTR authority, provider-incompatible DKIM bounds, incomplete
   special-use validation, and secret-path disagreement. Fixed at `e35d7bc`.
2. Rejected oversized report-generated records and incomplete production-zone
   policy. Fixed at `7a36b04` using global provider record bounds, mailbox
   bounds, pinned PSL data, and the IANA special-use registry policy.
3. Rejected unvalidated derived `mta-sts.<zone>` length. Fixed at `551f3c5`.
4. Rejected IPv4-mapped IPv6 and omitted current non-global IANA ranges. Fixed
   at `6a18960`.
5. Rejected interface-scoped IPv6 in AAAA desired state. Fixed at `f8b5725`.
6. Rejected RFC 5322 mailbox syntax masquerading as an exact RFC 6068 URI.
   Fixed at `6dfdf10` with a closed unescaped ASCII V1 policy.
7. Rejected missing RFC 5321 64-byte local-part bound. Fixed at `83be1e1`.
8. Rejected relative DNS owners that overflow after joining the admitted zone.
   Fixed at `79a1547`.
9. CLEAN.
10. Independent CLEAN/ACCEPT.

Every fix commit passed targeted race, vet, and diff checks. Final retained
runtime evidence covers the exact provider process, Caddy listener ownership,
and Authentik 2026.5.2 import behavior. No rejected finding was waived.

## Admission boundary

Acceptance applies only to the local composition child. It does not clear
provider publication, PTR, Authentik application, live identity/mail,
recovery, security, accessibility, or owner-acceptance gates.
