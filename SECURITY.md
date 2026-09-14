# Security policy

Do not open a public issue containing credentials, deployment manifests from a
live installation, hostnames that are not already public, or vulnerability
details. Report vulnerabilities privately to the maintainers.

The V0 planner is intentionally read-only. Any future apply capability must be
default-deny, bind approval to an immutable plan digest, constrain adapter
permissions, journal every transition durably, redact secrets structurally,
and fail closed after an unknown outcome.
