# Public website verification

Date: 2026-09-14

Base commit: `98b16f7fca0d01dc90282e1ed8fba84adc7bca3a`

Candidate source-state SHA-256: `6227e2c1de2850b5abf0d4f461c74a795cefb9353ca1217131efcb1e87de7479`

The source-state hash covers the exact staged full-index binary diff from base
commit `98b16f7fca0d01dc90282e1ed8fba84adc7bca3a`. It excludes review/evidence
records, the two status indexes that cite the hash (`docs/traceability.md` and
the global coverage map), and ignored scratch.

## Toolchain and hosts

- Agent host: Go `1.26.6-X:nodwarf5`, Node `26.7.0`, npm `12.0.2`, templ
  `0.3.1020`, Tailwind CSS `4.3.3`, HTMX `2.0.10`, Chromium.
- Development host: cached exact Go `1.26.6 linux/amd64` toolchain. Its default
  Go `1.26.5-X:nodwarf5` and Node `26.5.1` do not satisfy the pinned generation
  contract, so frontend generation ran only on the matching agent host.
- Agent root capacity before work: 4% bytes, 1% inodes; unchanged at handoff.
- Development capacity before verification: root 24% bytes/64% inodes; ZFS
  `/tank` 6% bytes/1% inodes.

## Passing gates

- `GOMAXPROCS=2 make verify-web` on the agent host: pinned clean npm install,
  templ generation, Tailwind generation, HTMX copy, double-generation hash
  equality, exact CSS content-address check, formatting, controller-import and
  side-effect scans, vet, focused race tests, coverage, and web build.
- The complete-page handler test requires exactly one ordinary anchor to
  `https://github.com/gotthboard/gotth-stack` with visible
  `View on GitHub` text.
- `WEB_EVIDENCE_DIR=/tmp/gotthstack-web-evidence
  scripts/verify-web-browser.sh`: process start/readiness/termination, complete
  HTML, HTMX fragment, JavaScript-disabled complete page, 1440x1100 capture,
  and 390x844 capture.
- Chromium DevTools Protocol click check: focused
  `#principle-link-recovery`, exercised the real HTMX request and inner swap,
  preserved that focused link, updated the panel to `data-topic=recovery`, and
  pushed `/principles?topic=recovery`.
- Exact Go `1.26.6` development-host gate: formatting, `go vet -mod=readonly
  ./...`, uncached `go test -mod=readonly -race -count=1 -cover ./...`, all
  command builds, controller example validation and planning, import/side-effect
  scans, and exact CSS content hash.
- `git diff --check -- .`
- `npm audit`: zero vulnerabilities in 35 audited packages.
- A fresh exact Go `1.26.6 linux/amd64` development-host run passed
  formatting, full vet, uncached race/coverage tests, all command builds,
  controller example validation/planning, the import boundary, and the exact
  `ab3aa925...` CSS content digest after the source-link change.

Development-host uncached race coverage:

- `cmd/gotth-stack`: 84.2%
- `cmd/gotthstack-web`: 56.0%
- `internal/site`: 76.4%
- `pkg/journal`: 84.3%
- `pkg/stack`: 94.5%

Every handwritten `internal/site` production function is covered at 100%.
The package total includes generated templ error paths. The web command gap is
the process-only `main` exit path and shutdown failures that require OS/socket
fault injection; real start, readiness, response, and graceful termination are
covered by the browser/process smoke. No public route, hostile-query class,
method boundary, security header, asset, HTMX response shape, or documented
shutdown success path is waived.

## Retained scratch evidence

- `.worker-evidence/wide.png`:
  `f18b7370954cfd3a76cee00710e936c578f70db4e3faf07042961d859a420fca`
- `.worker-evidence/narrow.png`:
  `52e662204cd2da4ef8b9d1596b36917217f711ace771ad22ed04e9f704fba2ab`
- `.worker-evidence/home.html`:
  `ba5a107b4b081bbd965ccb2369cf487a141b4f2e64f85908011e60970b7bf775`
- `.worker-evidence/no-js.html`:
  `e6f01a7c5bdb136b1aac7d81bcf6cd0109a6766a192510f53f826d62bd22ec5d`
- `.worker-evidence/principles-recovery.html`:
  `5e039b3881162f3efe5580a5da0286532a85e049771e291435bcb46e7ebc8a71`
- `.worker-evidence/web-benchmark.txt`:
  `97d6b69875aca367c506a75aff2174b1432ed44ac27c637c1112b9971acc3724`

These paths are gitignored review scratch and are not canonical product state.

## Finish cleanup

- Removed the exact task-owned local `node_modules` tree (21 MiB, 534 regular
  files), generated root `gotthstack-web` binary (9 MiB), temporary browser
  capture directory, and temporary coverage profile.
- Removed the exact development verification copy
  `/tank/development/linus/gotthstack-web-verify-20260914T1320Z` (4.8 MiB,
  104 regular files) after copying its required benchmark evidence locally.
- Retained the source worktree, canonical clone, bounded npm/Go/context-broker
  caches, 844 KiB of gitignored evidence, and the scoped handoff note. Those
  are source, reproducible cache, or required evidence rather than disposable
  output.
- Post-cleanup agent root remains 4% bytes/1% inodes. Development remains 24%
  root bytes/64% root inodes and 6% `/tank` bytes/1% `/tank` inodes.

## External state

At verification time no DNS record, live host, Caddy configuration, deployed
service, credential, tag, release, Forgejo branch, pull request, or canonical
`main` ref had changed. The owner subsequently authorized publication and
deployment as a separate operator action.
