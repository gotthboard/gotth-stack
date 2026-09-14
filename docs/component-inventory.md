# Initial component inventory

Snapshot: 2026-09-13. These revisions constrain the first design review; they
are not dependency pins or release claims.

| Repository / line | Observed revision | Reusable boundary |
| --- | --- | --- |
| `gotth-infrastructure/main` | `9abafdb2cf6c93f1cd823e91b0e66ca84555664a` | deterministic least-privilege application Compose rendering and effective inspect validation; deliberately no deployment |
| `gotth-authentik/main` | `77c811c18fe3c107f6f3e97ce3f9a3850a685300` | secret-free Authentik application desired state and exact comparison; deliberately no remote apply |
| `gotth-release/main` | `43e64e693a6fa3d0c1b045d6730fefa1ca767998` | immutable source/artifact identity and deterministic archives; no publication or deployment |
| `gotth-pg-migrate/main` | `bafb20b5aee66222fb7dd0d6abe7ee970cdb7878` | forward-only PostgreSQL 17 migration and ledger verification; application schema and rollback remain consumer-owned |
| `gotth-oidc/main` | `1ae119e52f8efc3392fcc9fa716b1b27c9ffce6c` | OIDC protocol validation; no product session authority |
| `gotth-scim/main` | `255629e27f7df301d263a116fb37fd315ff54693` | SCIM protocol validation; no product lifecycle persistence |
| `gotth-jobs/main` | `1c7b229f24e61a65c477408282aed74602dd3cab` | bounded job mechanism; not automatically the stack operation journal |
| `gotth-extensions/main` | `783b735679a541323cebebbc9ad0220fd4826fc3` | extension manifest/grant/handshake/health foundation; no deployment authority |
| GOTTH Mail unfinished 1.0-alpha line | `dc6b039eda6379ada698d3bfac9db2043915f578` | Mail product and identity/control seams; not product `main` and not yet deployed |
| `gotth-bb/main` | `86e25a1999bef86f97203a26e873895c75870e7d` | Board product; deployed topology is evidence, not a generic installer |

The controller may eventually pin and compose these mechanisms. It must not
copy them into one repository, infer compatibility from branch names, or
pretend that their individual render/check APIs collectively form a safe
transaction. The missing journal, adapter, backup, rollback, and recovery
boundaries remain real work.
