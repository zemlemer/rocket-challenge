# Rockets

Backend for the rockets challenge (task: [docs/challenge.md](docs/challenge.md)).
Requirements and design live in [openspec/](openspec/).

The system is a single executable, `rockets`, whose mode is chosen by a
subcommand:

```sh
rockets --help            # list modes
rockets server --help     # options of a mode
rockets processor        # inbox processing
```

| Option | Environment | Default |
|---|---|---|
| `--database-url` | `DATABASE_URL` | — (required) |
| `--db-connect-timeout` | `DB_CONNECT_TIMEOUT` | `30s` |
| `--log-level` | `LOG_LEVEL` | `info` |

A flag overrides its environment variable. Exit status: `0` success, `1` the
mode failed, `2` invalid invocation.

## Running the stack

```sh
docker compose up --build -d
curl localhost:8088/healthz
```

| Service | What it does |
|---|---|
| `postgres` | PostgreSQL 16 on a named volume, host port `15432` |
| `migrate` | one-shot `rockets migrate`, runs after PostgreSQL is healthy |
| `server` ×3 | `rockets server`, start after PostgreSQL is healthy and `migrate` succeeded, no published ports |
| `processor` ×4 | `rockets processor`, start after PostgreSQL is healthy and `migrate` succeeded, no ports |
| `nginx` | entry point on `localhost:8088`, round-robin over the servers, retries a failed request on another replica |

Point the provided simulator (the binary from the challenge ZIP; keep it as
`tools/rockets-simulator`, which is gitignored) at it:

```sh
./tools/rockets-simulator launch "http://localhost:8088/messages"
```

## Server mode

`rockets server` (`--http-addr` / `HTTP_ADDR`, default `:8088`) refuses to
start on a database that is not fully migrated and shuts down gracefully on
`SIGINT`/`SIGTERM`.

| Endpoint | Response |
|---|---|
| `POST /messages` | `202` once the request body is stored in the inbox; `503` if storage is unavailable, `500` on other failures. Never `4xx`: senders redeliver a message unchanged, so content is not validated here. |
| `GET /healthz` | liveness: `200 {"status":"ok"}` while the process serves HTTP; does not check the database |

Unknown paths get `404`, wrong methods `405`, both as `{"error": "..."}`.

Every body is stored as is; bodies over 1 MiB are stored truncated. The four
`metadata` fields are parsed and stored in their own columns (`channel`,
`message_number`, `message_time`, `message_type`); a malformed field is stored
as `NULL` without rejecting the request, and the `message` payload is not
parsed. A body whose identity (`channel`, `messageNumber`) is already in the
inbox is not stored twice. Inspect the inbox:

```sh
docker compose exec postgres psql -U rockets -d rockets -c \
  "select id, channel, message_number, message_time, message_type, truncated, left(encode(body, 'escape'), 80) as body from inbox order by id desc limit 5"
```

## Processor mode

`rockets processor` turns inbox entries into rocket state. It has no options of
its own: batch size (1,000 entries per transaction), poll interval for an empty
inbox (100 ms) and shutdown timeout (15 s) are fixed in
[internal/processor](internal/processor/processor.go). It serves no HTTP.

Each batch is one transaction: claim the oldest unprocessed entries
(`FOR UPDATE SKIP LOCKED`, so processors never take the same entry), validate
them, apply them, mark them. Entries are never deleted; every entry ends with
an outcome:

| `outcome` | Meaning |
|---|---|
| `applied` | the message changed its rocket's state |
| `quarantined` | the message is not valid (`reason` names the rule) and changed nothing |
| `failed` | a valid message the system could not apply (`reason` holds the error); a bug to investigate |

How a rocket's state is computed, independently of arrival order:

- **speed** = launch speed + increases − decreases, applied as increments, never clamped;
- **mission** from the message with the highest number (`mission_message_number`);
- **launch** only as message number 1, and message 1 only as a launch;
- **explosion** once and terminal; a further `RocketExploded` is quarantined;
- **`last_applied_message_number` / `last_applied_message_at`**: the newest applied message.

`by` and `launchSpeed` must be integers from 0 to 1,000,000,000.

Inspect processing:

```sh
docker compose exec postgres psql -U rockets -d rockets -c \
  "select outcome, count(*) from inbox group by 1"
docker compose exec postgres psql -U rockets -d rockets -c \
  "select id, channel, message_number, message_type, reason from inbox where outcome <> 'applied' order by id desc limit 10"
docker compose exec postgres psql -U rockets -d rockets -c \
  "select channel, rocket_type, speed, mission, explosion_reason, last_applied_message_number from rockets order by channel"
```

## Database migrations

Migrations are forward-only SQL files in
[internal/postgres/migrations](internal/postgres/migrations/), embedded into the
executable. `rockets migrate` applies the missing ones in version order; it is
safe to re-run and to run concurrently.

In the stack, the `migrate` service applies them on every `docker compose up`:

```sh
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

End-to-end tests run against the compose stack (`make test-e2e` starts it).
They write to the inbox and stop and restart server containers. A full default
run of the simulator is included when `tools/rockets-simulator` exists, or when
`ROCKETS_SIMULATOR` points at the binary elsewhere.

The simulator test logs the achieved rate (about 4,000 messages/s on a laptop
with Docker Desktop). Set `E2E_MIN_RATE` (messages/s) to fail the test below a
threshold; when it fails, `request_time` in `docker compose logs nginx` shows
whether the servers or the client were slow.
