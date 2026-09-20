# Provider-extension admission PRD

## Problem

GOTTH Stack must consume one DNS provider without turning an extension name,
archive, manifest, or Unix socket into authority. The admitted GoDaddy source
candidate exists independently, but Stack currently has no consumer that binds
its artifact, foundation grant, runtime configuration, or local transport.

## Scope

This feature adds one closed GoDaddy DNS admission profile. It verifies an
immutable artifact, negotiates the pinned foundation manifest, emits canonical
runtime binding/configuration documents, and proves the compiled provider can
authenticate and answer control health over its owner-only Unix socket.

## Requirements

- `STACK-EXT-001`: Accept only the exact extension ID
  `gotth.dns.provider.godaddy`, interface `gotth.dns.records` major 1, control
  major 1, secret `godaddy.pat`, and the four DNS-record capabilities.
- `STACK-EXT-002`: Verify a bounded deterministic Linux/amd64 archive by exact
  archive digest, versioned root, fixed member set, modes, member digests, and
  canonical foundation manifest. Reject links, devices, traversal, duplicate
  names, trailing compressed data, and expansion beyond fixed limits.
- `STACK-EXT-003`: Bind exact Forgejo/GitHub repository identities, identical
  source commits, immutable version, archive digest, manifest digest,
  executable digest, configuration-metadata digest, and foundation commit.
- `STACK-EXT-004`: Negotiate a non-empty exact capability subset, the one DNS
  interface, and the required PAT slot with `gotth-extensions` pinned at
  `3822dd722bc8606b8843d0a68c7c3e9f598d9ccc`.
- `STACK-EXT-005`: Emit canonical duplicate-free runtime binding and provider
  configuration bytes. Secret values, arbitrary paths, endpoints, headers,
  methods, commands, and environment variables are impossible inputs.
- `STACK-EXT-006`: Prove the exact provider executable accepts the generated
  binding/configuration over a private Unix socket and refuses a missing or
  incorrect service token. Conformance uses a fake PAT and performs no DNS
  RPC or external network call.
- `STACK-EXT-007`: Fail closed when cross-forge parity or release identity is
  absent. A locally built candidate may pass conformance but cannot become a
  release admission.

## Non-goals

- Provider implementation inside Stack.
- Generic extension discovery, registry, executor, archive installer, process
  manager, capability grammar, URL transport, shell, plugin loading, or Docker
  socket access.
- Live GoDaddy credentials, DNS mutation, public DNS propagation, certificate
  issuance, or production deployment.
- Claiming publication while the GitHub repository/tag/artifact is absent.

## Acceptance

The exact GoDaddy provider candidate passes archive, negotiation, canonical
binding/configuration, redaction, hostile archive, and authenticated compiled-
process conformance. Source admission and release admission remain distinct.
