# Cold review 1

## Decision

Narrow and retry.

## Findings

The first contract described the broad topology but left four boundaries too
vague to admit:

1. SMTP/IMAP TLS and authentication behavior on 25/465/587/143/993 was not
   explicit.
2. “Signed or independently verified” artifact provenance claimed no concrete
   signing authority and did not bind canonical mirror parity.
3. Mailbox and queue backup called for quiescence without defining the actual
   stop/drain/checkpoint/capture/resume sequence.
4. Database ciphertext and extension secret revisions were named, but disaster
   recovery of the external master keys/provider credentials had no separate
   custody contract.

The pass also required proxy-protocol trust to be restricted to the private
front endpoint, PTR authority to remain distinct from zone DNS authority, and
the beta acceptance matrix to include exact-sender OpenPGP with no unsigned
fallback plus monitoring and accessibility gates.

## Resolution

The PRD, architecture, implementation specification, workflow decomposition,
and traceability map were corrected. No runtime or permission change was made.
