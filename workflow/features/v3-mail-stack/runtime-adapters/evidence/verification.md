# Mail runtime adapter verification — 2026-09-20

## Candidate

Runtime implementation through `370aabd94e8e5f9057f2602936642329a05749dd`.
The exact release-manifest compatibility repair is
`3f0fd2144d431a5670a2e2763942dd57ba55fba2`.

Mail source candidate: `12dbe59973cba53e52d56437f21424b2ec53f3c8`.
Forgejo and GitHub both resolved
`workflow/feature/release.1-0.production-artifacts` to that commit.

## Exact artifacts

- Rspamd image:
  `ghcr.io/gotthboard/gotth-mail-rspamd@sha256:fe7b4f7a3db8ce9c453ad7585e1a752fbd109b5d36d7a35c2c236bd33d457fca`;
- canonical release manifest SHA-256:
  `d1f9886faa654a037aa2f0437ef52c982c91131a93e4969ad225fb9d0780aa08`;
- deterministic configuration archive SHA-256:
  `fa03341166fea00adef9e01d53816d982b103639f80ab9861f7b7f85d28fd331`;
- build identity: Go 1.26.6, linux/amd64, source epoch `1789938083`.

Mail's release assembler produced the manifest and archive twice with identical
bytes. The manifest binds all five exact role digests and all eight exact
configuration members. The Stack parser rejects unknown fields, duplicate
keys, noncanonical JSON, invalid role order, floating images, invalid build
identity, and a missing or non-positive build epoch.

## Runtime proof

On development host `10.0.0.97`, the root-only disposable test consumed the
canonical manifest and archive above. It installed the exact Rspamd image,
replaced its controller-secret revision, closed and reopened the adapter after
mutation, verified the candidate, rolled back to the previous running
container, and retained the SQLite Bayes state:

```text
go test -mod=readonly -race -count=1 \
  -run '^TestDisposableRspamdReplacement$' -v \
  ./internal/adapters/mailruntime
PASS: TestDisposableRspamdReplacement (50.70s)
```

The other role-specific differences are covered by exact argument and
effective-container matrices: public front ports, root-only Postfix/Dovecot
capabilities, Dovecot's bounded `SYS_CHROOT`, role users, immutable roots,
private tmpfs ownership, mount modes, secret slots, health entrypoints, and
image labels. Full-topology lifecycle proof remains correctly assigned to the
later acceptance child.

## Gates

```text
go vet -mod=readonly ./internal/adapters/mailruntime
PASS

go test -mod=readonly -race -cover -count=1 ./internal/adapters/mailruntime
PASS; 73.5% statement coverage

go test -mod=readonly -race -count=20 -shuffle=on ./internal/adapters/mailruntime
PASS (29.340s)

PATH=<checksum-verified-node-26.7.0>:$PATH make verify
PASS
```

The full gate used Go 1.26.6, Node 26.7.0, and npm 12.0.2. It passed
deterministic frontend generation, source-boundary checks, vet, all race and
coverage tests, command builds, example validation, and deterministic
planning. A second full run passed from a fresh detached worktree at the exact
reviewed workflow commit, and the tracked tree remained clean afterward.
Package coverage remained 73.5% for Mail runtime, 94.5% for the plan
kernel, 84.3% for the journal, 81.8% for Caddy, 79.4% for PostgreSQL, 72.8%
for Authentik, and 76.4% for the public site.

## Honest limits

This child proves typed runtime mechanisms, not controller authorization or a
complete deployment. It created only disposable containers and a private
bridge on the development host; cleanup removed them. It did not pull images,
delete volumes or persistent Mail data, create credentials, mutate DNS, expose
a public listener, tag a release, or touch a live deployment.

The missing controller, identity/provider composition, full-stack acceptance,
backup/restore, production hostname, and owner physical acceptance remain
explicit later work. They are not smuggled into this completion claim.
