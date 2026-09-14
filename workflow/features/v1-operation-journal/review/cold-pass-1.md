# Cold review pass 1 — journal correctness and recovery

Reviewed journal code source:
`4a140590ed90472caad93d9d526fd4bbc1d89eb2`

Decision: **CLEAN**

This pass began again after the canonical API list was corrected to include
`Approval` and `ActiveOperation`. It reviewed the requirements, public API,
storage framing, append ordering, replay, state transitions, tests, and exact
development-host evidence as one system.

Load-bearing checks:

- one approval is immutable, exact-plan-bound, secret-revision-bound,
  expiration-checked, and usable by exactly one operation ID;
- one installation admits at most one unfinished operation;
- intent reaches the synced log and synced atomic head before a caller may
  perform an effect;
- any failed persistence checkpoint poisons the open handle and forces replay;
- replay refuses committed ambiguity or corruption and repairs only the two
  documented uncommitted cases: a partial tail behind the head or one complete
  record ahead of it;
- read-only interruption permits only an exact successor attempt, while a
  mutation interruption requires explicit reconciliation;
- possibly live mutation cannot be hidden behind an ordinary failed terminal;
  it must be compensated or reported recovery-required;
- the public recovery queries return deep copies and do not require a second
  authority database.

The ZFS race gate, every-checkpoint reopen tests, 100 race repetitions,
shuffle, fuzz, corruption corpus, and explicit coverage accounting support the
claims. No unresolved correctness, durability-model, state-machine, or
contract mismatch remains in this workstream.
