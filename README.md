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
| `GET /rockets/{channel}` | `200` with one rocket (see [Reading rockets](#reading-rockets)); `404` when there is none |
| `GET /rockets` | `200 {"rockets": [...]}`, sortable; `400` for invalid sort parameters |

Unknown paths get `404`, wrong methods `405`, both as `{"error": "..."}`.
`HEAD` is accepted wherever `GET` is.

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

A channel may contain only ASCII letters, digits, `-`, and `_` (1–255 bytes).
The processor quarantines messages with other channel characters, naming
`metadata.channel` in the reason; they never change rocket state. Intake still
stores and answers them with `202`, including duplicate deliveries. Rocket rows
created before this rule may remain in the database; the read API hides them
and leaves them stored. Quarantined entries are not replayed automatically.

Inspect processing:

```sh
docker compose exec postgres psql -U rockets -d rockets -c \
  "select outcome, count(*) from inbox group by 1"
docker compose exec postgres psql -U rockets -d rockets -c \
  "select id, channel, message_number, message_type, reason from inbox where outcome <> 'applied' order by id desc limit 10"
docker compose exec postgres psql -U rockets -d rockets -c \
  "select channel, rocket_type, speed, mission, explosion_reason, last_applied_message_number from rockets order by channel"
```

## Reading rockets

The server reads the state the processors have stored:

```sh
curl -s localhost:8088/rockets
curl -s 'localhost:8088/rockets?sort=speed&order=desc'
curl -s localhost:8088/rockets/193270a9-c9cf-404a-8f83-838e71d9ae67
```

```json
{
  "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
  "type": "Falcon-9",
  "status": "active",
  "speed": 3500,
  "launchSpeed": 500,
  "mission": "ARTEMIS",
  "launchedAt": "2022-02-02T18:39:05.86337Z",
  "explosionReason": null,
  "lastMessageNumber": 12,
  "lastMessageAt": "2022-02-02T18:41:10.12345Z"
}
```

`GET /rockets` returns `{"rockets": [...]}` with every visible rocket in this
form, without pagination.

**Which rockets are visible.** A rocket appears once its launch or its
explosion has been processed. A rocket with only speed or mission changes so
far is `404` and not listed. An exploded rocket whose launch has not been
processed yet is shown with `type`, `launchSpeed`, `launchedAt` (and `mission`,
unless a mission change arrived) as `null`, and with a `speed` that holds the
increments without the launch speed; when the launch arrives, those fields fill
in and `speed` grows by the launch speed. Rockets whose channel breaks the
channel rule are hidden (`404`), and channels are matched exactly, case
included.

**Fields.** `status` is `active` or `exploded`. `lastMessageNumber` and
`lastMessageAt` belong to the highest-numbered applied message; lower numbers
may still be missing. Times are the senders' message times, not processing
times, in UTC, truncated to microseconds; a time whose UTC year falls outside
0000–9999 is written with all digits of its year (`10000-01-01T00:30:00Z`).

**Sorting.** `sort` is one of `channel` (default), `type`, `mission`, `speed`,
`status`, `launchedAt`; `order` is `asc` (default) or `desc`. Values are
case-sensitive. Text compares byte-wise (`Z` before `a`), `active` sorts before
`exploded`, `null` values come last in either direction, and ties are ordered
by ascending `channel`. An unknown, empty or repeated `sort`/`order`, or a
malformed query string, gets `400`; other parameters are ignored.

**Errors.** `404 {"error": "rocket not found"}` for an unknown or hidden
rocket; `503` when the database is unavailable or does not answer within 5 s;
`500` on other failures. nginx does not retry a read's `503` on another server
(only connection failures), so a client waits for one 5 s timeout, not three.
A query string that cannot be parsed (a bad `%` escape, or `;` as a
separator) gets `400` naming the cause.

**Consistency.** Reads show committed processing results: a `202` from
`POST /messages` does not mean the next read includes that message, which
appears once a processor has applied it. Each
response comes from one database snapshot, reads never block processing, and
they keep working while processors are stopped.

**Limitations.**
- The list is unbounded: any sender can add rockets by posting launches or
  explosions on new channels.
- `speed` is exact in JSON, but values beyond 2^53 lose precision in
  JavaScript clients.
- No CORS or caching headers: a browser dashboard on another origin needs a
  proxy.

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

For a longer simulator run, set the message count (the default is 100,000):

```sh
E2E_SIMULATOR_MAX_MESSAGES=200000 make test-e2e
```

Both simulator E2E tests use this count. The default run remains unchanged when
the variable is unset.
