# Contributing

Forgejo is the canonical development source. Keep one concern per change and
follow the repository's `workflow.toml` dependency order. Product requirements,
architecture, and implementation contracts must constrain code before it is
written. Every external mutation path requires preview, exact confirmation,
durable recovery state, hostile tests, and a reviewed rollback boundary.

Never commit credentials, live configuration, host inventories, database
contents, deployment receipts, or unredacted logs.
