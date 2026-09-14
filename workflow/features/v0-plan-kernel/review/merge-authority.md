# Cold review 2 — merge authority

## Decision

Accept with constraints.

## Problem

GOTTH Mail and GOTTH Board need one controller to compose deployment state
without turning a runtime extension into a root-level bootstrap mechanism.

## Admissibility

The V0 slice is small enough to review and solves the first real problem:
strictly identify desired components and derive one deterministic dependency
plan. It does not pretend that planning is deployment.

## Judge notes

The first cold pass corrected reconstructible manifest-digest validation,
canonical empty-set encoding, and dishonest URL/signature wording. The exact
corrected source then passed hostile development-host verification.

## Operator notes

V0 opens one bounded regular input file and writes validation or plan output.
It has no secret-value schema, adapter loading, network client, command runner,
host inspection, Docker/systemd path, or apply operation. Repetition is
side-effect free. Live state and existing product userspace are untouched.

## Documentation notes

The PRD, architecture, implementation specification, workflow dependencies,
coverage map, example, and changelog agree with the implementation. Existing
GOTTH component contracts were inventoried at exact revisions rather than
treated as an imaginary transaction API.

## Conditions

No apply command may be added until V1 proves durable intent, exact approval,
fsync/recovery, idempotency, and unknown-outcome handling. Concrete adapters
must remain separate and least-privilege. Mail and Board must retain separate
product adapters, databases, credentials, and failure domains.

## Final ruling

Accept the non-mutating V0 foundation; reject any claim that it can deploy a
host yet.
