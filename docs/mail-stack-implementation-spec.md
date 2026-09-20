# GOTTH Mail stack implementation specification

Source requirements: [mail-stack-prd.md](mail-stack-prd.md)

Source architecture: [mail-stack-architecture.md](mail-stack-architecture.md)

## Feature decomposition

1. `v3.mail-stack.contract` freezes the topology, artifact, listener, secret,
   backup, recovery, and verification contracts.
2. `v3.mail-stack.runtime-adapters` implements the five typed role adapters and
   disposable single-role/replacement tests.
3. `v3.mail-stack.controller` binds exact approvals and journal transitions to
   the admitted platform and mail adapters. It adds no generic executor.
4. `v3.mail-stack.identity-provider` composes Caddy, Authentik tenant desired
   state, OIDC/SCIM, certificate ownership, and an independently admitted DNS
   provider extension.
5. `v3.mail-stack.acceptance` proves complete disposable install, upgrade,
   backup, isolated restore, rollback, interrupted-operation recovery,
   multi-domain mail flow, and identity lifecycle.

Only one child is active. A child cannot use a planned successor as evidence.

## Release manifest

Canonical JSON is strict, duplicate-key rejecting, at most one MiB, and uses
schema version 1. It contains:

- exact product version and source commit;
- exact annotated tag identity plus canonical Forgejo/GitHub ref parity proof;
- five named role image repository digests;
- configuration archive SHA-256 and member manifest;
- minimum/current/maximum schema versions;
- exact extension repository, version, artifact, and manifest digests;
- build toolchain identity and archive SHA-256.

Every list is sorted. Unknown roles, duplicate members, floating image tags,
missing digests, mirror disagreement, unrecognized schema edges, and
version/tag mismatch reject.
The manifest digest becomes the Stack artifact digest; the configuration
archive digest becomes the configuration digest.

## Typed role adapters

`internal/adapters/mailruntime` contains an unexported fixed Docker mechanism.
Five exported constructors return distinct role types; callers cannot select a
role with a string after construction. Each constructor requires an exact
role-specific specification and exposes:

```text
Preflight(ctx, request) -> staged summary
Observe(ctx, operationID) -> observation
StopPrevious / RenamePrevious / CreateCandidate / StartCandidate
VerifyCandidate
StopCandidate / RemoveCandidate / RestorePreviousName / StartPrevious
VerifyPrevious
```

Each mutating call performs one transition and is idempotent only for its exact
predecessor or successor state. Create uses fixed arguments and `--pull never`.
Observation parses bounded Docker JSON and compares the complete effective
state. Error values are fixed sentinel classes and never include paths,
container output, hostnames, addresses, or secret data.

Transaction roots use owner-only directories, one advisory lock, canonical
JSON, file and directory sync, and operation-derived names. The adapter never
deletes persistent data. It may remove only an exact stopped operation-derived
container after its successor or rollback state is proven.

## Controller binding

The controller registers a closed map of exact adapter identities. A manifest
component must match one registered typed adapter and an exact capability
subset. Approval projects every component, artifact/configuration digest,
capability, and secret revision into `pkg/journal`.

For every adapter call the controller:

1. observes current state;
2. writes the exact step intent;
3. performs one bounded call;
4. observes and classifies the result;
5. writes the result digest or fixed reason; and
6. stops on unknown outcome.

Preflight is read-only except for private transaction staging and is recorded
as such. Runtime/container, route, configuration, migration, and restore calls
are mutations. The CLI does not accept arbitrary adapter names, commands,
arguments, paths, or environment variables.

## Production artifact contract

GOTTH Mail must publish prebuilt images and complete configuration templates.
Production images contain every daemon package at build time. Startup performs
no package manager or network fetch. The production profile has no reference
fixture, example domain, default password/token, Telegram fixture, public
Rspamd port, Roundcube dependency, or plaintext secret environment value.

The product image exposes bounded health and version endpoints. Postfix,
Dovecot, and Rspamd configurations consume product-generated files and
file-mounted secrets. The custom webmail is served by the product HTTP route;
Roundcube remains a reference-only compatibility fixture.

## Verification

Required tests trace `STACK-MAIL-001` through `STACK-MAIL-016` and include:

- strict release-manifest parser and hostile archive/member tests;
- per-role exact Docker argument and effective-inspection matrices;
- permissions, symlink, hardlink, owner, mode, descriptor-swap, and secret
  revision checks;
- every forward, reverse, duplicate, conflict, crash-visible mixed, and
  recovery state for each role;
- journal write-before-effect and mutation-ambiguity reconciliation;
- disposable real-role installation and digest-to-digest replacement;
- full topology install, active health, upgrade, backup, isolated restore,
  rollback, restart, and interrupted-mutation recovery;
- two-domain SMTP/IMAP/webmail/DKIM/policy/queue/automatic-mail flows;
- live disposable OIDC/SCIM lifecycle and Authentik-outage independence;
- no-reference-fixture, no-floating-artifact, no-secret-disclosure, no-shell,
  no-image-pull, no-Docker-socket, and no-generic-executor source checks;
- race, repeat, shuffle, coverage, vet, build, clean-copy, and two cold reviews.

Production apply is a separate evidence run using the exact already-passed
candidate. A production host is never a substitute for disposable acceptance.
