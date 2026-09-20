# Mail stack controller

This child binds the already-admitted Caddy, PostgreSQL, Authentik, and five
GOTTH Mail role mechanisms to exact plan approval and the durable operation
journal. The registry is closed in source. Artifact, configuration,
capability, secret-slot, and secret-revision drift rejects before approval is
recorded.

The controller records preflight inspection, private staging, every live
mutation, candidate verification, rollback mutation, and previous-state
verification as separate steps. Restart recovery retries interrupted read-only
calls with a new attempt and only observes interrupted mutations. It never
reinvokes an unresolved mutation.

The fixed Mail network is a separate recovery-only operation. It cannot be
mixed with rollbackable role replacement. The CLI remains read-only; this
child adds no generic executor, apply command, live deployment, credential,
hostname, DNS change, tag, or release.

Implementation source is `148659dbc7956d6d0d96852b3fea42aab231c857`.
Exact verification and remaining limits are in `evidence/verification.md`.
