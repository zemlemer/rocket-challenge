# Proposal

## Why

Every later part of the rockets system — intake, processing, queries — stores its data in PostgreSQL. The system will run as one binary in several modes (`migrate`, `server`, `processor`), and the schema must be created by a dedicated step before any server or processor starts. This first iteration delivers that foundation: the binary with its `migrate` mode, the migration mechanism, and a compose setup that runs PostgreSQL and the migration. Tables arrive with the features that need them, starting with the inbox in the next change.

## What Changes

- A single `rockets` binary whose mode is selected by a subcommand; this iteration provides `migrate` (later iterations add `server` and `processor`). Subcommands, flags, environment variables and help come from the lightweight CLI library `kong`.
- A migration mechanism: versioned, forward-only SQL files embedded in the binary with Go `embed`, applied in order, recorded in the database, safe to re-run and safe when several processes migrate at the same time.
- A Dockerfile and a docker compose file with PostgreSQL and a one-shot `migrate` service that runs after PostgreSQL is healthy and exits.
- Integration tests against the real database.

Not in this iteration: any table (the production migration set is empty; the inbox comes with change `add-rocket-state-tracking`), HTTP endpoints, `server` and `processor` modes, domain logic.

## Capabilities

### New Capabilities

- `database-schema`: how the schema is versioned and migrated.
- `application-cli`: the `rockets` binary, its modes, configuration, exit codes, and the `migrate` mode.
- `deployment`: container image and docker compose stack (this iteration: PostgreSQL and the migration service).

### Modified Capabilities

None (greenfield project).

## Impact

- New Go module with `cmd/rockets` and `internal/postgres` (runner and embedded migrations).
- New dependencies: `github.com/alecthomas/kong` (CLI), `github.com/jackc/pgx/v5` (PostgreSQL).
- New files: `Dockerfile`, `compose.yaml`, `Makefile`, `.gitignore`, `internal/postgres/migrations/` (embedded, empty of migrations for now).
- Change `add-rocket-state-tracking` builds on this: it adds the inbox and the other tables as migrations `0001`–`0003`, the `server` and `processor` modes, and extends the compose stack.
