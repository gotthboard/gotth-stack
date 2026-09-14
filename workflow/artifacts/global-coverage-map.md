# Global coverage map

| Subsystem | Risk | Required harness | Current evidence | Known gap |
| --- | --- | --- | --- | --- |
| strict manifest parsing | critical | unit, hostile, fuzz, size/depth | plan-kernel evidence: exact verified source `baec0eb`; parser package functions 88.2–100% | unreachable decoder token-type/error branches remain uncovered; fuzz and hostile cases cover attacker-controlled paths |
| dependency planning and digests | critical | unit, property, repeat, shuffle | plan-kernel evidence: 100 race repeats, 20 shuffled runs, digest consistency tests | JSON marshal failures for fixed structs are not inducible through the public contract |
| CLI file/output boundary | high | unit, integration, negative | plan-kernel evidence: Linux Go 1.26.6; CLI package 84.2% | process `main`, close failure, and narrow file-race branches are not unit-injected; `run` and external CLI paths pass |
| durable journal and recovery | critical | binding, crash, filesystem fault, corruption, lock, replay, state machine, boundary, fuzz | exact source `4a140590`; ZFS race gate, 100 race repeats, 20 shuffled runs, two 30-second fuzz targets; `pkg/journal` 84.3% statements | direct OS failure branches, impossible fixed-struct marshal failures, and unreachable defensive branches remain; every durability checkpoint affecting recovery and every attacker-controlled parser path practical to drive is covered; no requirement exception |
| public website | medium | unit, route, hostile input, browser, no-JS, accessibility, process smoke | candidate `5a340915`; exact organization link, full handwritten handler coverage, development race gate, real HTMX click, narrow/wide/no-JS captures | generated templ error branches and OS-only process failure exits are not line-covered; public behavior is covered |
| platform adapters | critical | disposable runtime, rollback | none | planned platform-adapter workstream; blocks apply |
| Mail stack | critical | disposable end-to-end mail/identity | none | planned mail-stack workstream |
| Board stack | critical | disposable end-to-end web/identity | none | planned board-stack workstream |
| administrator UI | critical | browser, no-JS, accessibility, security | none | planned administrator-UI workstream |
| provider extensions | critical | conformance, auth, least privilege | none | planned provider-extension workstream |

Unimplemented critical mutation boundaries block an apply command. They are
not exceptions and cannot be hidden by plan-kernel coverage.
