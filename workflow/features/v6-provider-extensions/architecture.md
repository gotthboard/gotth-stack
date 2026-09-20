# Provider-extension admission architecture

## Closed mechanism

```text
Mail identity composition
        |
        | exact DNS admission request
        v
internal/extensions/godaddydns
  artifact verifier -> foundation negotiation -> canonical runtime documents
        |
        | fixed executable name + fixed environment-name set
        v
gotth-extension-godaddy-dns process
  owner-only Unix gRPC + service token
```

There is one profile and one provider. Shared foundation code supplies only
manifest/grant/session compatibility. Provider-specific DNS RPC code remains in
the provider repository and the later identity-composition adapter; this
feature does not grow a generic invocation surface.

## Artifact boundary

The verifier reads at most 32 MiB compressed and 64 MiB expanded. It admits one
top-level directory named from the exact version and these regular members:

- `gotth-extension-godaddy-dns` mode 0755;
- `manifest.json`, `configuration-metadata.json`, `LICENSE`, and `SHA256SUMS`
  mode 0644.

All tar names are canonical. There are no links, sparse files, devices, extra
members, PAX expansion, duplicate members, set-ID bits, group/other write bits,
or trailing gzip streams. `SHA256SUMS` must be canonical and cover the other
four members exactly.

## Authority binding

The distribution proof contains two fixed repository URLs and a status enum.
`candidate` requires the exact Forgejo commit and an empty GitHub commit;
`published` requires both commits to equal the pinned source commit. This is
data validation, not a network lookup; release tooling must supply retained
remote evidence. Candidate conformance can run without publication, but only
`published` can become release-ready.

Foundation negotiation selects only granted DNS capabilities, control 1.0,
interface 1.0, and `godaddy.pat`. The generated runtime binding repeats the
exact capability subset alongside manifest, grant, and session digests because
a digest is evidence, not executable policy.

## Process conformance

The conformance harness stages the already-verified executable and writes only
private fixed-name files. It supplies a fake PAT, starts the executable with no
arguments and the five documented environment paths, connects to the private
socket, proves unauthenticated rejection, then proves bound handshake and
health. It sends no DNS RPC, follows no network path, and terminates the child.

The production supervisor remains part of identity composition. This feature
proves its exact documents and transport contract; it does not add an apply
command or generic long-lived process supervisor.
