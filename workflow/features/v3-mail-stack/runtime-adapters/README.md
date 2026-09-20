# Mail runtime adapters

State: `done`

Implements the five fixed GOTTH Mail role adapters over one private Docker
mechanism. Each role has a closed image repository, user, capability, port,
mount, secret, health, observation, replacement, and rollback contract.

The adapters pull no image, expose no arbitrary command or environment input,
delete no persistent state, and grant no controller apply authority. The exact
verification and two clean cold-review records are under `evidence/` and
`review/`. The separately active controller child must bind these mechanisms
to durable approval and journal transitions before any apply path exists.
