# Maintainer review

Two cold passes were applied to the complete Caddy slice.

## Findings corrected

- Persistent install residue was initially invisible to verification and
  rollback. Observation now exposes it, verification requires absence, and
  restore validates, removes, and directory-syncs safe residue.
- A candidate could initially disable or move the Admin API and strand its own
  verification and rollback path. Previous and candidate adapted JSON now
  require the exact configured listener.
- Relative paths initially inherited the controller process directory. The
  service working directory is now explicit, validated, command-pinned, and
  included in the adapter binding digest.
- The process seam initially accepted an arbitrary argument slice. It now
  accepts only three enum-shaped requests whose arguments are package-built.
- Atomic replacement initially allowed umask to narrow the service-readable
  mode. The implementation now explicitly preserves UID, GID, and exact mode
  before writing and verifies them after rename.
- The executable was initially path-bound but not artifact-bound. It now
  requires an exact SHA-256 digest on open and before every command, rejects
  symlinks and hard links, and checks parent ownership and mode.
- The import detector initially considered only the first token on each line.
  It now rejects a bare unquoted `import` token anywhere in the Caddyfile.
- Adding Go adapter source initially polluted Tailwind's automatic repository
  scan and changed public CSS accidentally. Tailwind input is now explicitly
  limited to templ sources and the content-addressed asset was regenerated.

## Final ruling

Accepted as a private, unstable-alpha adapter candidate. The mechanism is
bounded, inspectable, reversible, and honest about crash state. Admission is
strictly limited to this child: the parent adapter workstream is incomplete,
the CLI has no apply authority, and no live Caddy configuration was touched.
