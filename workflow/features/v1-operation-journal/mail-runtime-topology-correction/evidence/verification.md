# Mail runtime topology correction evidence

Verification target: `c9e6545ca15bb347ab4b084a3a7797c12f389a30`.

The dedicated development host cloned the exact Forgejo branch and passed:

- exact Go 1.26.6 toolchain check;
- formatting, vet, full race/coverage tests, and binary build through
  `make verify`;
- example validation and two byte-identical plan renders;
- TOML parsing, feature/parent/folder/dependency checks, JSON event parsing,
  and the one-active-feature invariant;
- explicit example assertions for GOTTH Mail, mail front/proxy, Postfix,
  Dovecot, Rspamd, and PostgreSQL, with no Mailu component;
- docs-only diff proof, clean worktree, and diff hygiene.

No planner source, runtime, credential, DNS record, live host, tag, release, or
GitHub mirror changed.
