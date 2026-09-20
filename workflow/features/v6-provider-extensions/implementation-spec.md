# Provider-extension admission implementation specification

## Package

`internal/extensions/godaddydns` exports one typed admission request and result.
The request contains only:

- exact candidate/published distribution proof; candidate requires an empty
  GitHub commit and published requires exact cross-forge commit parity;
- UUID instance ID;
- sorted granted capability subset;
- sorted exact zones and mutable record-type subset;
- production/OTE enum and timeout 1–30 seconds.

No map or free-form metadata enters a canonical document.
The version, repository identities, source and foundation commits, and all
artifact/member SHA-256 values are compiled admission-profile pins rather than
caller inputs.

## Validation and output

`VerifyAndAdmit` reads the archive once into a bounded buffer, verifies its
digest, parses the gzip/tar stream, hashes fixed members, parses the foundation
manifest, constructs a foundation grant/host profile, negotiates the session,
and emits:

- exact executable bytes and digest;
- canonical runtime binding JSON;
- canonical provider configuration JSON;
- manifest, grant, and session digests;
- an admission digest covering the distribution proof and all emitted digests;
- release-ready boolean, true only for published exact cross-forge parity.

Errors are fixed categories and contain no archive text, member data, zone,
path, token, PAT, remote URL query, or provider response.

## Conformance command

The cross-repository test requires explicit paths to an exact clean provider
repository and a new artifact output directory. It verifies source commit
`b313b413dc9ea7d495fa45b21b26c6e3dc94b064`, builds the deterministic candidate
artifact, feeds it to the Stack admission package, stages the verified binary,
and performs control handshake/health only.

No live credential or zone is read. The candidate is not marked release-ready
until a retained GitHub commit/tag/artifact proof exists.

## Verification

- accepted candidate and published-parity vectors;
- every manifest/grant/config/distribution mismatch;
- archive traversal, link, device, duplicate, mode, member, checksum, size,
  gzip, and trailing-data matrices;
- deterministic binding/configuration/admission digests;
- descriptor scan for secret/free-form fields;
- compiled-process missing/wrong/correct token and binding mismatch;
- repeat, shuffle, race, fuzz, vet, build, clean-copy, and two cold reviews;
- source scan forbidding generic exec, plugin, shell, caller URL/header/method,
  provider implementation, and live DNS access in production code.
