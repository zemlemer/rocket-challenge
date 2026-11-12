# Design

## Context

First iteration of the rockets system (see proposal.md). The overall architecture — an API server writing an inbox and a processor consuming it, both on one PostgreSQL — is described in change `add-rocket-state-tracking`. Here we lay the foundation: one executable with modes, the `migrate` mode, the migration mechanism, and a compose stack with PostgreSQL and a migration step. No table is created yet; the inbox comes with the next change.

## Goals / Non-Goals

**Goals:**
- One binary, one image; modes chosen by subcommand, so later iterations add `server` and `processor` without new build or deployment plumbing.
- A migration runner that later iterations extend only by adding `.sql` files.
- Schema changes as an explicit deployment step, not a side effect of starting a server.
- An integration-test harness against a real PostgreSQL that later iterations reuse.

**Non-Goals:**
- Down migrations / rollback (forward-only; a mistake is fixed by a new migration).
- Any table (the production migration set is empty in this iteration); any mode other than `migrate`.

## Decisions

### D1. One executable, modes as subcommands, CLI via `kong`

`cmd/rockets/main.go` defines a kong CLI struct with one field per mode (`Migrate MigrateCmd \`cmd:""\``; `Server`/`Processor` follow later). Shared options (`--database-url` / `DATABASE_URL`, `--db-connect-timeout` / `DB_CONNECT_TIMEOUT`, `--log-level` / `LOG_LEVEL`) live on the root struct with `env:` tags, giving flag-over-env precedence for free. Kong's own exit status for usage errors is 80, so `main` does not let kong exit: it takes the parse error, prints the error and usage to stderr and returns status 2 as the spec requires; `--help` goes through kong's `Exit` hook and returns 0. Each command's `Run(ctx, *Globals, *slog.Logger) error` returns an error; `main` maps it to exit status 1. Whether a mode needs the database is declared by the mode (a marker method), and the missing-URL check runs after parsing so that `--help` never requires it.

*Alternatives considered:*
- `spf13/cobra` — the de-facto standard but heavier: builder API, `pflag`, and env binding needs `viper` or hand-written code.
- `urfave/cli/v3` — capable, but env sources and flags are declared in parallel structures; more boilerplate than kong's struct tags.
- `peterbourgon/ff/v4` — very light and env-friendly, but still beta (`v4.0.0-beta.1`).
- stdlib `flag` with `FlagSet` per subcommand — no dependency, but help, env binding and error handling would be hand-written in every mode.

Kong (stable v1.x, no runtime dependencies, declarative struct tags) is the lightest option that covers subcommands, env vars and help.

### D2. Migrations embedded with Go `embed`, hand-written runner

Migrations live in `internal/postgres/migrations/NNNN_name.sql` and are embedded with `//go:embed migrations` into an `embed.FS`, so the binary carries its schema and the image needs no extra files. The directory holds a `README.md` describing the naming convention; it keeps the embed pattern valid while no migration exists yet, and the runner only considers `*.sql` files. The runner takes an `fs.FS` (tests pass their own sets, including a broken one), parses versions from file names, sorts them and applies the missing ones. Applied versions are recorded in `schema_migrations(version bigint primary key, name text not null, applied_at timestamptz not null default now())`.

*Alternatives considered:* `pressly/goose` or `golang-migrate` (both can read an `embed.FS`) — mature, but a dependency plus its own conventions and locking semantics to verify, for about 80 lines of code here. Revisit if migrations grow numerous or need down steps.

### D3. Concurrency: session advisory lock around the whole run

The runner acquires a dedicated connection, calls `pg_advisory_lock(<fixed key>)`, reads applied versions, applies the missing ones, then unlocks. A concurrent run blocks on the lock and then finds nothing to do. The lock is session-scoped, so it is released if the process dies. Compose runs a single `migrate` container, but the guarantee matters for manual runs and future orchestrators.

### D4. One transaction per migration

Each file runs in its own transaction together with its `schema_migrations` insert (PostgreSQL DDL is transactional): a failing file leaves no trace and the run stops with an error naming it. Migrations that cannot run in a transaction (e.g. `CREATE INDEX CONCURRENTLY`) are out of scope until needed.

### D5. Connection retry

`pgxpool.New` followed by `Ping` in a loop with exponential backoff (100 ms doubling, capped at 2 s) until `DB_CONNECT_TIMEOUT`. Compose already orders `migrate` after the PostgreSQL health check; the retry covers manual runs and orchestrators without such ordering.

### D6. Image and compose

- Dockerfile: multi-stage — `golang` builder with module cache, `CGO_ENABLED=0`; final stage `gcr.io/distroless/static-debian12:nonroot` with `ENTRYPOINT ["/rockets"]`.
- `compose.yaml`:
  - `postgres` (`postgres:16`, named volume `pgdata`, `pg_isready` health check, published on host port `${ROCKETS_PG_PORT:-15432}` for local tests — not 5432, which is commonly taken by other local PostgreSQL instances);
  - `migrate` (built image, `command: ["migrate"]`, `DATABASE_URL` pointing at `postgres`, `depends_on: postgres: condition: service_healthy`, `restart: "no"`).
  - Later services declare `depends_on: migrate: condition: service_completed_successfully`.

### D7. Logging

`log/slog` with a text handler on stderr; level from `LOG_LEVEL` (default `info`).

### D8. Test harness

Integration tests read `TEST_DATABASE_URL`; when unset they are skipped with a message naming the variable and `make db-up`. Each test creates a fresh database (`CREATE DATABASE test_<random>`, dropped afterwards), so tests are independent and can run in parallel. CLI behavior (help, unknown mode, missing URL, exit codes) is tested by running the built binary from the test with `os/exec`.

## Risks / Trade-offs

- [Hand-written runner has fewer features than a library] → small scope, covered by tests for ordering, repeatability, concurrency and failure.
- [Advisory lock key collides with another application on the same database] → fixed 64-bit key derived from the project name; the database is dedicated to this system.
- [Forward-only migrations] → accepted; schema mistakes are corrected by a new migration.
- [Kong's exit codes and messages are library behavior] → pinned by the CLI tests, so an upgrade that changes them is caught.

## Migration Plan

Greenfield: `docker compose up --build` starts PostgreSQL and runs `migrate` once. For tests: `make db-up`, then `make test-integration`.
