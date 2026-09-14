# Implementation specification

## V0 public API

Canonical package: `github.com/gotthboard/gotth-stack/pkg/stack`.

```go
func ParseManifest(io.Reader) (Manifest, error)
func BuildPlan(Manifest) (Plan, error)
func MarshalPlan(Plan) ([]byte, error)
```

The module root contains no Go package. `cmd/gotth-stack` imports only the
canonical package.

## Bounds and validation

- Input: at most 1 MiB; exactly one JSON value; maximum nesting depth 32.
- Stack/component/secret IDs: lowercase ASCII slugs, 1–80 bytes.
- Adapter identity: `gotth-stack-adapter-<slug>.v<positive integer>`, at most
  160 bytes.
- Artifact source: a 1–2048 byte canonical `https` or `oci` URL with a host and
  path, and without user information, query, fragment, surrounding whitespace,
  or backslashes. It is an inert label in V0 and is never opened or executed.
- Artifact and configuration digests: lowercase `sha256:` plus 64 hex digits.
- Components: 1–64. Dependencies/capabilities/secret slots: at most 64 each.
- Capability: dot-separated lowercase ASCII tokens, at most 160 bytes.

All set-like values must be unique. Component IDs are unique. Dependencies
must refer to another declared component. Kahn's algorithm with a sorted ready
set creates the stable order and detects cycles.

## Deterministic encoding

Normalization copies input before sorting. `encoding/json` marshals only fixed
structs—never maps—so field order is stable. Manifest digest hashes compact
normalized JSON. Plan digest hashes compact JSON with the digest field empty.
`MarshalPlan` emits indented JSON followed by one newline.

## CLI

```text
gotth-stack validate <manifest.json>
gotth-stack plan <manifest.json>
```

The input path must name one regular, non-symlink file no larger than one MiB.
`validate` prints only `valid` and the manifest digest. `plan` writes the
deterministic plan only after full success. Usage/validation errors go to
stderr and return nonzero without echoing file contents or manifest values.

No `apply`, `exec`, plugin loading, network client, shell, Docker, systemd, or
privileged filesystem API exists in V0.

## Planned apply state machine

```text
planned -> approved -> preflighting -> applying -> verifying -> complete
                                   \-> failed -> rolling_back -> rolled_back
                                                \-> recovery_required
```

This state machine is documentation only until the durable journal and
recovery feature is active. V0 code must not expose its mutations as stubs.
