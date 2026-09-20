# Mail stack controller verification — 2026-09-20

## Candidate

Implementation and tests are complete through
`148659dbc7956d6d0d96852b3fea42aab231c857`. The initial binding commit was
`9abe8800f8d97abbce936093395fc4cfd39b8bd0`; the authority/recovery correction
was `977b9b39538753d834a08136d75956e3a8098c3b`.

## Proven boundary

- the closed registry accepts only the eight compiled adapter identities;
- typed request artifact and configuration digests, exact capabilities,
  secret slots, and secret revisions must equal the approved plan;
- mutation intent is durable before every effect;
- interrupted mutations are observed and classified, never reinvoked;
- interrupted preflight, staging, and verification receive explicit next
  attempts after reopen;
- known failures roll back actions in each binding's fixed safe order and
  components in reverse dependency order;
- terminal rollback requires a fresh previous-state verification for every
  mutated component;
- recovery-only network creation is isolated from rollbackable operations;
- verifier errors cannot be hidden by a coarse state observation;
- exact duplicate terminal execution returns the durable result; and
- production controller source contains no shell, generic command, plugin,
  reflection, unsafe, or caller-selected executable surface.

## Gates

```text
go test -race -count=1 ./internal/controller ./internal/adapters/caddy \
  ./internal/adapters/mailruntime ./pkg/journal
PASS

go test -count=20 ./internal/controller
PASS

go test -shuffle=on -count=10 ./internal/controller
PASS

make verify-controller
PASS; controller 53.6%, journal 84.5%

PATH=<checksum-verified-node-26.7.0>:$PATH make verify
PASS
```

The exact final commit passed Go 1.26.6 race, vet, build, source-boundary,
deterministic frontend generation, CLI validation/planning, and repository
regression gates. Node 26.7.0 and npm 12.0.2 were pinned for the frontend
portion. The tracked tree remained clean.

## Coverage limit

Controller statement coverage is 53.6%, below the preferred target. The
uncovered bulk is the bodies of concrete typed adapter closures. Their
mechanisms are separately covered in Caddy (81.6%), PostgreSQL (79.4%),
Authentik (72.8%), and Mail runtime (73.5%) packages. Controller tests cover
the authorization projection, exact state predicates, write-before-effect,
known and unknown outcomes, read-only retry, mutation reconciliation,
recovery-only refusal, terminal idempotency, and multi-component reverse
rollback. Complete real-topology composition remains assigned to the
acceptance child; no percentage is manufactured here.

## Honest limits

This child provides an internal Go execution boundary, not a public apply
surface or deployment. Network creation still requires its own explicitly
approved operation and has no generic rollback. No live host, Docker policy,
credential, DNS record, certificate, release, or tag changed. DNS-provider
admission, identity composition, complete disposable lifecycle proof, and
owner physical acceptance remain mandatory.
