# V6 provider extensions

State: `done` for candidate admission and conformance.

Admit the one independently reviewed GoDaddy DNS provider through the shared
extension compatibility boundary without broad host authority. Governing
requirements are in `prd.md`, `architecture.md`, and
`implementation-spec.md`.

The exact provider source candidate is
`b313b413dc9ea7d495fa45b21b26c6e3dc94b064`; its Forgejo evidence head is
`ed2707a`. GitHub publication is absent, so candidate conformance may proceed
but release admission must remain blocked.

Stack implementation source is
`e2f72a3a25299e7e48e131ac151c151b0f5ac856`. The production profile compiles
publication availability as false, so repeating the expected GitHub commit
cannot manufacture release readiness. Exact verification and remaining limits
are recorded in `evidence/verification.md`.
