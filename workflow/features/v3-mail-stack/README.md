# V3 Mail stack

In progress. The production contract and five typed role adapters are
complete. The journal-bound controller is active; identity/provider
composition and disposable install/upgrade/backup/restore/rollback acceptance
remain downstream work.

Mailu, the reference Compose fixtures, runtime package installation,
development secrets, and Telegram fixtures are not production components.

The admitted runtime mechanism is private and closed: it cannot pull images,
accept arbitrary commands, or acquire controller authority. Its exact evidence
is under `runtime-adapters/`.
