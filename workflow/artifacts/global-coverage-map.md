# Global coverage map

| Subsystem | Risk | Required harness | Current evidence | Known gap |
| --- | --- | --- | --- | --- |
| strict manifest parsing | critical | unit, hostile, fuzz, size/depth | V0 evidence: exact verified source `baec0eb`; parser package functions 88.2–100% | unreachable decoder token-type/error branches remain uncovered; fuzz and hostile cases cover attacker-controlled paths |
| dependency planning and digests | critical | unit, property, repeat, shuffle | V0 evidence: 100 race repeats, 20 shuffled runs, digest consistency tests | JSON marshal failures for fixed structs are not inducible through the public contract |
| CLI file/output boundary | high | unit, integration, negative | V0 evidence: Linux Go 1.26.6; CLI package 84.2% | process `main`, close failure, and narrow file-race branches are not unit-injected; `run` and external CLI paths pass |
| durable journal and recovery | critical | crash, filesystem fault, replay | none | planned V1; blocks apply |
| platform adapters | critical | disposable runtime, rollback | none | planned V2; blocks apply |
| Mail stack | critical | disposable end-to-end mail/identity | none | planned V3 |
| Board stack | critical | disposable end-to-end web/identity | none | planned V4 |
| administrator UI | critical | browser, no-JS, accessibility, security | none | planned V5 |
| provider extensions | critical | conformance, auth, least privilege | none | planned V6 |

Unimplemented critical mutation boundaries block an apply command. They are
not exceptions and cannot be hidden by V0 planner coverage.
