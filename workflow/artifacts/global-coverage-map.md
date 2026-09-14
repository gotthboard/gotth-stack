# Global coverage map

| Subsystem | Risk | Required harness | Current evidence | Known gap |
| --- | --- | --- | --- | --- |
| strict manifest parsing | critical | unit, hostile, fuzz, size/depth | V0 tests | none after V0 admission |
| dependency planning and digests | critical | unit, property, repeat, shuffle | V0 tests | none after V0 admission |
| CLI file/output boundary | high | unit, integration, negative | V0 tests | no platform matrix until V0 verification |
| durable journal and recovery | critical | crash, filesystem fault, replay | none | planned V1; blocks apply |
| platform adapters | critical | disposable runtime, rollback | none | planned V2; blocks apply |
| Mail stack | critical | disposable end-to-end mail/identity | none | planned V3 |
| Board stack | critical | disposable end-to-end web/identity | none | planned V4 |
| administrator UI | critical | browser, no-JS, accessibility, security | none | planned V5 |
| provider extensions | critical | conformance, auth, least privilege | none | planned V6 |

Unimplemented critical mutation boundaries block an apply command. They are
not exceptions and cannot be hidden by V0 planner coverage.
