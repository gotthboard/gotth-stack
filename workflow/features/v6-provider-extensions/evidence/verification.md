# Provider-extension admission verification — 2026-09-20

## Candidate

The original Stack admission was complete through
`e2f72a3a25299e7e48e131ac151c151b0f5ac856`. Identity composition refreshes
that closed pin set to exact provider source
`c1f3525753da982a3ba84a5f443ae8e35dadebc8`, retained Forgejo evidence head
`8529e8d13dcd7634c6810b1b937fb652564283a4`, candidate version
`1.0.0-alpha.2`, and archive SHA-256
`88ce5fb3c2bd2dc1f66707a8657886055632a31e0a4e7e1cec8e459e0742435f`.
The refreshed metadata and Stack type allowlist include bounded SRV mutation.

GitHub repository, tag, and artifact evidence do not exist. The Stack
production profile therefore compiles publication availability as false.
Candidate conformance is admitted; release admission remains unavailable.

## Proven boundary

- the compressed archive, versioned root, member set, USTAR form, modes,
  ownership, member digests, and canonical checksum file are exact and bounded;
- links, traversal, duplicates, extra members, nonzero trailing data, extra
  gzip streams, and oversized input fail closed;
- the foundation dependency is pinned to commit
  `3822dd722bc8606b8843d0a68c7c3e9f598d9ccc` and negotiates only the one control
  protocol, one DNS interface, required PAT slot, and requested capability
  subset;
- caller input cannot choose an executable, path, command, environment name,
  URL, header, method, repository, version, digest, or secret value;
- canonical binding/configuration and the admission digest bind instance,
  capabilities, zones, record types, environment, timeout, artifact, grant,
  session, and distribution state;
- the exact compiled provider refuses missing and incorrect service tokens,
  refuses a mismatched session binding, and answers authenticated handshake and
  health over an owner-only Unix socket; and
- conformance uses a fake PAT, calls no DNS RPC, performs no GoDaddy request,
  and checks both stdout and stderr for token/PAT disclosure.

## Gates

```text
PATH=<pinned-node-26.7.0>:$PATH TMPDIR=/tmp make verify
PASS; handwritten godaddydns admission package 81.9% statement coverage

go test -race -count=100 ./internal/extensions/godaddydns/...
PASS

go test -race -shuffle=on -count=20 ./internal/extensions/godaddydns/...
PASS

go test -fuzz=FuzzVerifyArtifact -fuzztime=10s ./internal/extensions/godaddydns
PASS; about 39,000 executions in the recorded run

GOTTH_GODADDY_PROVIDER_REPO=<exact-provider-repository> make provider-conformance
PASS

fresh clone at e2f72a3a25299e7e48e131ac151c151b0f5ac856:
make verify && make provider-conformance
PASS; tracked tree remained clean
```

The full Go 1.26.6 gate also passed deterministic web generation, npm audit,
all existing adapter/controller/journal race tests, vet, builds, and CLI
validation/planning. The generated protobuf package reports 0% on descriptor
initialization code; this is generated transport scaffolding, not untested
admission policy. Handwritten admission, hostile archive, request, negotiation,
and process-boundary behavior is covered.

## Review corrections

Review removed an unsynchronized child-process state read, covered stdout as
well as stderr redaction, asserted the socket file type, added binding-mismatch
conformance, parenthesized the tar type predicate, corrected an overbroad
source scan that matched generated protobuf internals, and added parser fuzzing.
A second authority pass rejected caller-asserted publication. The final profile
requires a compiled publication-availability pin backed by retained evidence;
that pin is false here.

## Honest limits

This feature admits a source candidate and its local transport contract. It is
not a process supervisor, DNS reconciler, certificate issuer, or deployment.
No live credential was read and no DNS record was observed or changed. GitHub
parity, immutable release tags/artifacts, real PAT transport, production
supervision, DNS propagation, TLS issuance, and live deployment remain for the
release and identity/acceptance work. Those omissions cannot be papered over by
setting a request field.
