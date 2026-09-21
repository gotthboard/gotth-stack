# V6 provider extensions

State: `done` for candidate admission and conformance.

Admit the one independently reviewed GoDaddy DNS provider through the shared
extension compatibility boundary without broad host authority. Governing
requirements are in `prd.md`, `architecture.md`, and
`implementation-spec.md`.

The exact provider source candidate is now
`c1f3525753da982a3ba84a5f443ae8e35dadebc8`; its Forgejo evidence head is
`8529e8d13dcd7634c6810b1b937fb652564283a4`. It supersedes the earlier
observation-only SRV candidate. GitHub publication is absent, so candidate
conformance may proceed but release admission must remain blocked.

Stack implementation source is
`e2f72a3a25299e7e48e131ac151c151b0f5ac856`. The production profile compiles
publication availability as false, so repeating the expected GitHub commit
cannot manufacture release readiness. Exact verification and remaining limits
are recorded in `evidence/verification.md`.
