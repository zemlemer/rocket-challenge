# Proposal

## Why

`server`, `processor` and `nginx` restart automatically (`restart: unless-stopped`), but `postgres` uses Docker's default policy and never restarts. After a database crash or a restart of the Docker daemon the stateless services come back while the database stays down: servers answer `503` or fail to start and restart in a loop, processors stop processing, and only a manual `docker compose up` recovers the stack. `depends_on` does not help here: it orders services only when `docker compose up` starts them.

## What Changes

- The `postgres` service gets `restart: unless-stopped`, the same policy as the other long-running services.
- `migrate` keeps `restart: "no"`: it is a one-shot job and the schema already exists on the volume.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployment`: the PostgreSQL service restarts automatically unless it was stopped explicitly.

## Impact

- One line in `compose.yaml`. No code, schema, API or image change.
- A database stopped with `docker compose stop postgres` stays stopped, as before.
