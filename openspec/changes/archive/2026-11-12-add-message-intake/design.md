# Design

## Context

Builds on the archived change `add-migrate-mode`: the `rockets` executable (kong, shared options, connection retry), the embedded migration runner, the image, and compose with PostgreSQL and a one-shot `migrate` service. The overall architecture — a server writing an inbox and a processor consuming it — is in change `add-rocket-state-tracking`; this iteration builds its intake half. Specs: `message-intake`, `http-server`, and additions to `database-schema` and `deployment`.

## Goals / Non-Goals

**Goals:**
- Lose nothing that was acknowledged; never reject a request because of its content.
- Server instances interchangeable behind a load balancer; losing one is invisible to senders.
- Learn the real message format from the simulator before writing validation and processing.

**Non-Goals:**
- Message validation, quarantine, processing, rocket state, `GET /rockets…` (change `add-rocket-state-tracking`).
- Container health checks inside the distroless image (no shell or curl); nginx detects failed instances passively.
- TLS, authentication, rate limiting.

## Decisions

### D1. Store first, parse metadata, leave the payload

The handler reads at most 1 MiB + 1 byte (`io.LimitReader`), sets `truncated` if the extra byte arrived (exactly 1 MiB is not truncated), and only then builds the entry. A read error before the limit is reached — client disconnect, `ReadTimeout`, malformed chunked encoding — aborts the request: nothing is saved and the handler answers `500` if it still can. Saving a partial body would let its identity shadow the complete redelivery ("first body wins"). All four metadata fields are parsed with a small, lenient decode; the `message` payload is never decoded:

```go
var probe struct {
    Metadata struct {
        Channel       json.RawMessage `json:"channel"`
        MessageNumber json.RawMessage `json:"messageNumber"`
        MessageTime   json.RawMessage `json:"messageTime"`
        MessageType   json.RawMessage `json:"messageType"`
    } `json:"metadata"`
}
```

`channel` counts only if it decodes to a non-empty string of at most 255 bytes without NUL: PostgreSQL `text` rejects NUL and a btree index entry is limited to about 2.7 KB, so either would make the insert fail on every redelivery. `messageNumber` counts only if its raw token matches `^[1-9][0-9]*$` and parses as int64 — so `1.5`, `1e3`, `"1"`, `0`, `-3` and overflow give no identity (`1e3` is numerically an integer, but accepting it buys nothing and complicates the rule). `messageTime` counts if it is a JSON string accepted by `time.Parse(time.RFC3339Nano, …)` (any number of fractional digits, `Z` or numeric offset) and is stored as `timestamptz`. `messageType` counts under the same rules as `channel` (non-empty, ≤ 255 bytes, no NUL); whether it is a known type is the processor's business, so unknown types are stored as given. Each field is kept or dropped on its own, so one malformed field does not hide the others; the identity needs both `channel` and `messageNumber`. Duplicate JSON keys follow `encoding/json` (last one wins). Any failure — invalid JSON, truncation, empty body, missing fields — yields an entry without identity; it is never an error.

### D2. Inbox schema and insert (`0001_inbox.sql`)

```sql
create table inbox (
  id             bigserial   primary key,
  received_at    timestamptz not null default now(),
  channel        text        null,
  message_number bigint      null,
  message_time   timestamptz null,      -- metadata.messageTime, null when malformed
  message_type   text        null,      -- metadata.messageType as sent, null when malformed
  body           bytea       not null,
  truncated      boolean     not null,
  check ((channel is null) = (message_number is null))
);
create unique index inbox_identity_uq on inbox (channel, message_number)
  where channel is not null;
```

Insert: `insert into inbox (channel, message_number, message_time, message_type, body, truncated) values ($1, …, $6) on conflict (channel, message_number) where channel is not null do nothing`. One autocommit statement: `202` is sent only after it returned without error, so the row is committed (`synchronous_commit` stays on). `bytea` because the body may be invalid JSON or truncated; the check constraint forbids half an identity; `id` order is insertion order for later oldest-first processing.

*Alternative considered:* `jsonb` body — would force rejecting invalid JSON, which the spec forbids.

### D3. Layering: server → handler → repository (interface owned by the handler)

A deliberately small clean architecture. The repository interface is declared by its consumer, the handler package, so the dependency points from the database code to the handler and never the other way (dependency inversion):

```
cmd/rockets          wiring: config, pool → repository → handlers → server; signals
internal/server      http.Server: routes, timeouts, JSON 404/405, graceful shutdown
internal/handler     HTTP handlers; owns InboxRepository, InboxEntry, Metadata, Identity, ErrUnavailable
internal/repository  implements handler.InboxRepository with SQL over a pgx pool
internal/postgres    database infrastructure: Connect (retry), migrations, RequireSchema
pkg/helpers          exported helpers used by several packages (JSON responses)
```

```go
// package handler
type Identity struct { Channel string; MessageNumber int64 }
type Metadata struct { Identity *Identity; MessageTime *time.Time; MessageType *string }
type InboxEntry struct { Metadata; Body []byte; Truncated bool }

// What the Messages handler needs from storage — nothing more.
type InboxRepository interface {
    Save(ctx context.Context, e InboxEntry) error // idempotent per identity
}

// ErrUnavailable marks storage failures worth a 503 (sender should retry).
var ErrUnavailable = errors.New("storage unavailable")
```

- `server` imports `handler` only for the shared JSON error shape (`WriteError`); routes see handlers as plain `http.Handler`.
- Import graph: `cmd/rockets → server → handler`; `cmd/rockets → repository → handler`; `cmd/rockets → postgres`. `handler` imports neither `server` nor `repository`, so it is tested with a fake repository; `repository` is tested against a real database.
- `postgres` holds what is about the database as such (connecting, schema versions); `repository` holds what is about the system's data (queries per use). `repository` uses the `*pgxpool.Pool` created by `postgres.Connect` and the `internal/testdb` harness in tests, but `postgres` knows nothing about handlers.
- Metadata parsing (D1) lives in `handler`: it is part of reading the HTTP request.
- `repository` wraps connection failures and timeouts with `handler.ErrUnavailable`; the handler maps it to `503` and any other error to `500`.

- Helpers: each package keeps its unexported helpers in `helpers.go` and its test helpers in `helpers_test.go`. A helper used by several packages moves to `pkg/helpers` and becomes exported (today: `WriteJSON`, `WriteError` — the JSON error shape shared by `handler` and `server`). Test helpers shared across packages (discarding logger, pool, migrated database, TCP proxy) live in `internal/testdb` instead: they import `testing`, which must not be linked into the `rockets` binary through `pkg/helpers`.

*Alternatives considered:* (a) a shared `repository` package with interfaces — the interface would belong to no consumer and every consumer would see every method; (b) a use-case layer between handler and repository — for intake it would only forward one call.

### D4. Schema version check

`postgres.RequireSchema(ctx, pool, minVersion)` reads `max(version)` from `schema_migrations` (missing table = version 0) and returns an error naming `rockets migrate` when it is lower. The server requires the latest embedded version at build time, i.e. the highest migration file it was compiled with. A newer database is accepted.

### D5. HTTP server

- Go `ServeMux` patterns `POST /messages`, `GET /healthz`; a catch-all handler and a wrapper turn the mux's plain-text 404/405 into JSON errors.
- `202` has no body; errors are `{"error": "..."}`. `handler.ErrUnavailable` → `503`; anything else → `500`; both logged with `slog`.
- `http.Server` timeouts: read header 5 s, read 30 s, write 30 s, idle 60 s; keep-alive on (Go default).
- `/healthz` is a liveness check: `200` while the process serves HTTP, no database access. Database availability is not a property of the process: it is reported per request (`503` on `POST /messages`), and compose starts servers only after PostgreSQL is healthy. A database-pinging health check would also make every replica report unhealthy at once during an outage, which nginx cannot route around anyway.
- Shutdown: on `SIGINT`/`SIGTERM` call `Server.Shutdown` with `--shutdown-timeout` (default 15 s), then close the pool. In-flight handlers run with a context detached from the signal, so a request that has started its insert completes it. If `Shutdown` hits its deadline, the remaining connections are closed and the process exits with status 1 ("shutdown was not clean"): unanswered senders redeliver, so nothing is lost, but the exit status tells the operator.

### D6. Database outage behavior

No special code: `pgxpool` drops broken connections and dials new ones on the next request, so after an outage intake recovers by itself. While the database is down, the insert fails within the connect timeout and the server answers `503`.

Pool settings are set explicitly by `postgres.Connect` unless the URL overrides them: `ConnectTimeout` 5 s (`connect_timeout`) — without it a silently dropping host would block a request until the 10 s save timeout — and `MaxConns` 10 (`pool_max_conns`) — pgx's default depends on the CPU count of the machine, which makes the total across replicas unpredictable. Three servers × 10 stays far below PostgreSQL's default `max_connections` of 100. Other pgxpool defaults are kept: connection lifetime 1 h, idle time 30 min, health check every minute.

### D7. nginx

```nginx
upstream api {
  zone api 64k;
  server server:8088 resolve max_fails=1 fail_timeout=5s;
  keepalive 32;               # reuse connections to the servers
  keepalive_timeout 50s;      # < server IdleTimeout (60 s)
}
resolver 127.0.0.11 valid=5s ipv6=off;
server {
  listen 8088;
  location / {
    proxy_pass http://api;
    proxy_next_upstream error timeout http_502 http_503 non_idempotent;
    proxy_next_upstream_tries 3;
    proxy_connect_timeout 1s;
    client_max_body_size 0;      # never 413; intake truncates
  }
}
```

- `keepalive` makes nginx reuse upstream connections (with `proxy_http_version 1.1` and an empty `Connection` header); without it nginx opens a new TCP connection per request — about 4,000 per second under the simulator's default load. The idle timeout is shorter than the server's `IdleTimeout`, so nginx never reuses a connection the server is just closing.
- `resolve` in an upstream needs open-source nginx ≥ 1.27.3; Docker DNS returns one address per replica and nginx re-resolves, so restarted replicas rejoin.
- `client_max_body_size 0`: nginx's default of 1m would answer `413` for larger bodies, breaking the never-4xx contract; truncation is the server's job.
- Retrying `POST` relies on nginx buffering the request body (`proxy_request_buffering on`, the default) so it can be resent to another replica; turning buffering off would silently disable retries of `POST`.
- Retrying `POST` (`non_idempotent`) is safe for identified messages: the unique index keeps one copy. An unidentified body may be stored twice on a retry after a lost response; it carries no identity, so it is only ever quarantined later. Accepted.
- An upstream `503` from a database outage is retried on other replicas, which fail the same way; the sender then gets `503` and redelivers later.

### D8. Compose

`server` service: same image, `command: ["server"]`, `deploy.replicas: 3`, `depends_on: postgres: condition: service_healthy` and `migrate: condition: service_completed_successfully`, `restart: unless-stopped`, `stop_grace_period: 20s` (Docker's default of 10 s would `SIGKILL` the server before its 15 s shutdown timeout), no published ports. `nginx` service: `nginx:1.29-alpine` (≥ 1.27.3), `ports: ["8088:8088"]`, the directory `deploy/nginx/` mounted read-only as `/etc/nginx/conf.d/` (a directory, not a single file: editors and `sed -i` replace the file's inode, which a single-file bind mount would not see), `depends_on: server`, `restart: unless-stopped`.

### D9. Test strategy

- Unit: identity extraction table (valid, missing, empty channel, non-string channel, NUL in channel, 255- and 256-byte channel, `1.5`, `1e3`, `"1"`, `0`, negative, int64 overflow, duplicate keys, invalid JSON, truncated, empty body); handler with a fake repository — every content variant yields `202`, repository errors yield `503`/`500`, an aborted body read saves nothing, exactly 1 MiB is not truncated and 1 MiB + 1 is, `GET /messages` `405`, unknown path `404`.
- Integration (real database): inbox constraints; repository insert/dedup; 20 concurrent identical posts store one row; two independent pools + handlers ("two instances") store each identity once; schema check on migrated/unmigrated databases; outage via a TCP proxy that is stopped and restarted — `503` during, `202` after, without recreating the pool. Tests spanning layers in process (handler + repository + database) live in `test/integration`, not in a layer's own package. There, identity edge cases go through the handler into the real database: a 255-byte channel and `messageNumber` 2^63−1 are stored with identity; NUL, a 256-byte channel, binary garbage and an empty body are stored without one; invalid UTF-8 in `channel` is decoded by `encoding/json` to U+FFFD and stored with that identity (two different invalid byte sequences could therefore map to the same channel — accepted, real channels are UUIDs).
- Binary: a malformed chunked body (invalid chunk size) stores nothing and gets `5xx`; a well-formed chunked redelivery is stored. A slow client hitting `ReadTimeout` takes the same code path as a disconnect and is not tested separately (30 s).
- Binary: `rockets server` on an unmigrated database exits 1 naming `rockets migrate`; post then `SIGTERM` during a slow request completes it and exits 0 (slow request simulated with a body sent in two parts).
- Stack connection handling (e2e): upstream reuse is checked on the configuration nginx actually loaded (`nginx -T`, comments stripped: `keepalive`, `keepalive_timeout` below 60 s, HTTP/1.1, empty `Connection`), plus a rough socket count — socket states alone proved unable to tell reuse from no reuse under Docker Desktop. The stop grace period is read from the container (`StopTimeout` > 15 s).
- Stack (`test/e2e`, build tag `e2e`, `make test-e2e`, against the running compose stack): POST through nginx stored; 2 MiB body gets `202`; requests spread over all replicas (upstream address in nginx's access log); a replica stopped during delivery causes no non-`2xx`; a replica whose process is killed from the Docker VM (not `docker stop`, which restart policies treat as intentional) is restarted; the default simulator run (`ROCKETS_SIMULATOR`) gets only `202` and leaves one inbox entry per identity.

## Risks / Trade-offs

- [Accepting everything hides a broken sender] → every stored entry without identity is logged at WARN with its size; the simulator notes check that real traffic always carries an identity.
- [Inbox grows until processing exists] → expected in this iteration; the next change consumes it.
- [nginx retry stores an unidentified body twice] → accepted (D7).
- [Requests right after `docker compose up` recreates the servers can get `502` for up to 5 s] → recreated replicas get new addresses while nginx still holds the old ones (`resolver valid=5s`); deploy-time only, senders redeliver. Lowering `valid` trades DNS load for a shorter window.
- [Intake throughput is bounded by commit latency] → each `POST` waits for its own WAL flush before `202` (required by Durable acknowledgement), so with the simulator's 3 concurrent senders throughput ≈ 3 / commit latency. Normally ~0.5 ms per request (≈ 4,300 msg/s); in one observed episode the same stack ran at 6–22 ms per request (≈ 230 msg/s) with no retries and no config difference, and it did not reproduce in four attempts — most likely disk-flush latency of the Docker Desktop VM. The e2e simulator test logs the achieved rate and fails below `E2E_MIN_RATE` when set. If it matters in production, group commit (`commit_delay`) or batching several requests per transaction would raise throughput without weakening the 202-after-commit rule.
- [No container health check] → nginx passive failure detection plus `restart: unless-stopped`; a hung-but-alive instance would only be detected by request timeouts.
- [Simulator format assumptions] → the simulator run in this iteration records the real format before validation is written.

## Migration Plan

`docker compose up --build`: PostgreSQL, `migrate` applies `0001_inbox`, then three servers and nginx. Rollback: none needed (forward-only; dropping the inbox would be a new migration).
