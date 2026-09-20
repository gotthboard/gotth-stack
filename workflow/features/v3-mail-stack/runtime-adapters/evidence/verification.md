# Mail runtime adapter verification — 2026-09-20

## Candidate

Admitted Stack source: `370aabd717d6557b3fa3cf498654415da625ff59`.

Mechanism repair source: `7183ec3005a6d484e4ebbd04c3508c54d4dd4709`.

GOTTH Mail source: `acd1100207f018623b9f2a7156ffed8d53c3222a`.

The Mail release assembler produced the same artifact bytes twice:

- configuration archive SHA-256:
  `fa03341166fea00adef9e01d53816d982b103639f80ab9861f7b7f85d28fd331`;
- release manifest SHA-256:
  `a954b47de79ea3b25330707adf3a0e0382fc2536dc006984ce987ffb0690daec`.

The Stack parser accepted that exact manifest and all eight archive members.

## Development-host verification

Host: `10.0.0.97`. Go: 1.26.6. Node: 26.7.0. npm: 12.0.2.

The following focused gates passed:

- `go test -mod=readonly -count=10 ./internal/adapters/mailruntime`;
- `go test -mod=readonly -race -count=3 ./internal/adapters/mailruntime`;
- `go test -mod=readonly -shuffle=on -count=10 ./internal/adapters/mailruntime`;
- `go vet -mod=readonly ./internal/adapters/mailruntime`;
- `make verify-mail`.

Focused race coverage was 73.5% of statements. The uncovered remainder is
primarily host-error injection and opt-in disposable setup; the changed output
bound, mount identity, role arguments, lifecycle transitions, and failure
paths have direct regression coverage.

`make verify` then passed from the complete candidate. That gate includes
deterministic frontend generation, source-boundary checks, repository-wide
formatting, vet, race coverage, command builds, example validation, and
deterministic planning. Recorded package coverage included:

- `internal/adapters/mailruntime`: 73.5%;
- `pkg/stack`: 94.5%;
- `pkg/journal`: 84.3%;
- Caddy adapter: 81.8%;
- PostgreSQL adapter: 79.4%;
- Authentik adapter: 72.8%.

## Real replacement proof

The opt-in `TestDisposableRspamdReplacement` used `/usr/bin/docker`, the exact
release manifest/archive above, and
`ghcr.io/gotthboard/gotth-mail-rspamd@sha256:7c2a9305873ba1ee574b4a9dc3a22d3f4b016e6ba483e10db3a22f799d2e1b45`.
The local image carried source label
`acd1100207f018623b9f2a7156ffed8d53c3222a`; no registry pull occurred.

The 47.8-second test proved fresh install, exact effective inspection,
readiness, controller-secret revision replacement, close/reopen recovery,
reverse rollback, and survival of durable SQLite Bayes state.

The first run exposed a false conflict: Linux directory `st_size` changed when
Rspamd created legitimate state. The binding now ignores size only for
directories while retaining device, inode, owner, group, and mode. Regular
file size remains bound, and a substituted directory inode is still rejected.

Mail's combined production smoke separately passed control plane, NGINX
TLS/auth, Postfix, Rspamd, Dovecot LMTP, Maildir, and authenticated IMAPS with
all five images built from the same Mail source. That proves product image
compatibility, not controller or live-deployment authority.

## Boundary result

Production source scanning proves `process.go` is the only command-execution
site and rejects shells, systemd, image pulls, destructive volume operations,
Docker-socket mounts, generic formatting paths, journal coupling, and an
`apply` path. No credential, hostname, DNS/PTR record, public image, tag,
release, live container, or production host changed.
