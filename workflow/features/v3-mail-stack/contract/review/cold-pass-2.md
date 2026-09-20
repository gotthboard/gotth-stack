# Cold review 2

## Decision

Accept with constraints.

## Admissibility

The contract now assigns every public listener and certificate to one owner,
defines TLS/authentication behavior, restricts client-address provenance,
binds exact Forgejo/GitHub release identity, keeps secret bytes outside plans
and journals, separates recovery-key custody, defines consistent backup and
isolated restore, and refuses hidden DNS/PTR or production mutation.

The five runtime roles remain explicitly typed rather than becoming a generic
Docker executor. Platform adapters retain their existing authority boundaries.
Mailu and development fixtures remain excluded. Failure, rollback, and unknown
outcome behavior are honest.

## Constraints

- This admits documentation and decomposition only.
- The active runtime-adapter child must provide real disposable evidence.
- The independently admitted DNS provider, production GOTTH Mail artifact,
  public hostname, PTR grant, monitoring destination, and backup custody remain
  blockers; none may be inferred from this contract.

## Final ruling

Accepted: the mechanism is explicit enough to constrain implementation without
granting apply authority.
