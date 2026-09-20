# Cold review 1

## Decision

Narrow and retry.

## Findings

The typed role surface was acceptably closed, but the first candidate lacked a
real Stack-owned replacement proof. Product smoke alone could not prove the
adapter transaction across install, replacement, process restart, reopen, and
rollback.

Adding that proof exposed a real mechanism defect: persistent-directory
identity included Linux `st_size`. A daemon creating a legitimate file changes
directory size, so the next journalable transition falsely reported a mount
replacement. This would strand a healthy deployment after first use.

The repository also lacked a dedicated gate preventing the private mechanism
from drifting into a shell, image puller, destructive volume tool, Docker
socket grant, journal-coupled executor, or CLI `apply` path.

## Required correction

Add one exact-artifact disposable replacement/reopen/rollback proof, bind
mutable directories by stable identity rather than content-dependent size,
retain regular-file size and directory-inode substitution checks, and make the
source boundary part of every full verification.
