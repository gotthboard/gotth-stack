# Cold review 1 — maintainer judge

## Verdict

Accept with constraints after correction.

## Exact flaws found

The plan validator accepted a syntactically valid replacement manifest digest
when the caller also recomputed the plan digest. It also accepted `null` for
set-like plan fields even though planner output canonically uses empty arrays.
The implementation specification incorrectly called artifact source labels
canonical URLs without implementing URL canonicalization.

## Corrections

- Reconstruct and verify the normalized manifest digest from every plan.
- Reject `null` dependency, capability, and secret-slot sets in plans.
- Describe artifact sources as bounded URL labels and explicitly state that
  digests are content identities, not publisher signatures.
- Add hostile regression tests for each corrected boundary and explicit length
  and invalid-UTF-8 rejection cases.

## Boundary notes

V0 remains non-mutating and has no adapter, network, command execution, Docker,
systemd, secret-value, or apply path. No userspace existed to break. The review
used the documented V0 contract and Go standard-library behavior; it did not
invent a deployment mechanism from folklore.
