# Rockets

Backend for the rockets challenge (task: [docs/challenge.md](docs/challenge.md)).
Requirements and design live in [openspec/](openspec/).

The system is a single executable, `rockets`, whose mode is chosen by a
subcommand:

```sh
rockets --help            # list modes
rockets migrate --help    # options of a mode
```

| Option | Environment | Default |
|---|---|---|
| `--database-url` | `DATABASE_URL` | — (required) |
| `--db-connect-timeout` | `DB_CONNECT_TIMEOUT` | `30s` |
| `--log-level` | `LOG_LEVEL` | `info` |

A flag overrides its environment variable. Exit status: `0` success, `1` the
mode failed, `2` invalid invocation.

## Database migrations

Migrations are forward-only SQL files in
[internal/postgres/migrations](internal/postgres/migrations/), embedded into the
executable. `rockets migrate` applies the missing ones in version order; it is
safe to re-run and to run concurrently.

With docker compose (PostgreSQL plus a one-shot `migrate` service that runs
after PostgreSQL is healthy and exits):

```sh
docker compose up --build -d
docker compose logs migrate
```

Locally, against the compose database:

```sh
make db-up
make build
./bin/rockets migrate --database-url 'postgres://rockets:rockets@localhost:15432/rockets?sslmode=disable'
```

PostgreSQL is published on host port `15432` (override with `ROCKETS_PG_PORT`).

## Tests

```sh
make test               # unit tests; integration tests are skipped
make db-up              # start PostgreSQL for integration tests
make test-integration   # all tests against the database
```

Integration tests need `TEST_DATABASE_URL` (set by `make test-integration`);
each test creates and drops its own database.
