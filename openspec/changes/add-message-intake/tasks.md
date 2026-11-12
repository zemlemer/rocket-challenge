# Tasks

## 1. Inbox schema

- [ ] 1.1 Add `0001_inbox.sql` (D2); verify with integration tests that after `rockets migrate` entries with and without identity are stored with receipt time and increasing ids, a duplicate identity is refused, two entries without identity are both kept, and half an identity is refused
- [ ] 1.2 Add `postgres.RequireSchema` (D4); verify with integration tests that a database at the latest version passes, an unmigrated one fails with an error naming `rockets migrate`, and a newer one passes

## 2. Intake

- [ ] 2.1 Add `internal/handler` with the `InboxRepository` interface, `InboxEntry`, `Identity`, `ErrUnavailable` (D3) and the lenient identity extraction (D1); verify with table-driven unit tests covering every identity case listed in D9 (incl. NUL, 255/256-byte channel, `1e3`, overflow, empty body)
- [ ] 2.2 Implement `handler.InboxRepository` in a new `internal/repository` package with the conflict-ignoring insert (D2) and the `ErrUnavailable` mapping; verify with integration tests: identified and unidentified entries stored, an empty body stored, a duplicate identity keeps the first body, the truncation flag persists, 20 concurrent identical inserts store one row, two independent pools inserting the same messages store each identity once

## 3. Handler and server

- [ ] 3.1 Add the handlers (`POST /messages` with bounded read and `202`/`503`/`500`, `GET /healthz`) and `internal/server` (routes, JSON `404`/`405`, timeouts, graceful shutdown) (D3, D5); verify with `httptest` and a fake repository that malformed JSON, unknown type, missing identity, an empty body, a 2 MiB body (stored truncated), a body of exactly 1 MiB (not truncated) and duplicates all get `202`, a body read aborted by the client saves nothing, repository failures get `503`/`500`, `/healthz` is `200` without touching storage, `GET /messages` → `405`, unknown path → `404`

## 4. Server mode

- [ ] 4.1 Add `rockets server` (`--http-addr`, connection, schema check, server timeouts, graceful shutdown; D5); verify with binary tests: unmigrated database → status 1 naming `rockets migrate`; a posted message lands in the inbox; `SIGTERM` during a slow request lets it complete with `202` and exits 0
- [ ] 4.2 Add the outage integration test (D6) in `test/integration`: server handler with a pool behind a TCP proxy; stopping the proxy gives `503` on `POST /messages` while `/healthz` stays `200`, restarting it gives `202` again without recreating the pool, and the redelivered message is stored once

## 5. Deployment

- [ ] 5.1 Add `server` replicas to `compose.yaml` (D8); verify `docker compose up --build` runs `migrate` to completion before three server containers start and log that they listen, and a killed server container is restarted
- [ ] 5.2 Add `deploy/nginx/default.conf` and the `nginx` service (D7, D8); verify `curl localhost:8088/healthz` is `200`, a 2 MiB `POST` through nginx gets `202` (not `413`), 30 requests are spread over all three replicas (server logs), a stopped replica causes no client-visible errors, and a restarted replica rejoins rotation
- [ ] 5.3 Run the provided simulator with default options against `localhost:8088`, once undisturbed and once stopping a replica mid-run; verify every request gets `2xx`, the inbox has one entry per distinct identity sent and no entries without identity; inspect stored bodies and record the observed format (message-number start, number types, `messageTime` format, message types, duplicates, out-of-order) in `docs/simulator-notes.md`
- [ ] 5.4 Update `README.md` (server mode, `POST /messages` contract, inbox inspection via `psql`, stack layout); verify every documented command runs as written

- [ ] 5.5 Automate the stack checks of 5.1–5.3 as end-to-end tests (`test/e2e`, build tag `e2e`, `make test-e2e`): POST through nginx stored, 2 MiB body not rejected, requests spread over all replicas, replica stopped during delivery, crashed replica restarted, default simulator run (with `ROCKETS_SIMULATOR`); verify `make test-e2e` passes against the compose stack

## 6. Connection handling

- [ ] 6.1 Set the pool's connect timeout (5 s) and size (10) in `postgres.Connect` unless the URL sets `connect_timeout` / `pool_max_conns` (spec application-cli: Database connection pool); verify with unit tests for defaults and overrides, and an integration test that a request against a black-holed address fails within the connect timeout
- [ ] 6.2 Add `--shutdown-timeout` / `SHUTDOWN_TIMEOUT` and exit status 1 when it is exceeded (spec http-server: Graceful shutdown); verify with a binary test where a request stays in flight past a 1 s timeout
- [ ] 6.3 Verify the server's connection timeouts and keep-alive (spec http-server: Connection timeouts and reuse) with binary tests: two requests on one connection, connection closed after 5 s without complete headers
- [ ] 6.4 Add upstream `keepalive` to nginx and `stop_grace_period: 20s` to the server service (D7, D8); verify with e2e tests that 200 requests open far fewer than 200 upstream connections and that the grace period exceeds the shutdown timeout

- [ ] 6.5 Make `/healthz` a liveness check without database access and start servers only after PostgreSQL is healthy (spec http-server: Liveness endpoint; deployment: Server replicas); verify with handler and integration tests (`200` during a database outage) and an e2e check of the compose start dependencies

- [ ] 6.6 Parse all four metadata fields in the handler and store them in `inbox` (`message_time`, `message_type` added to `0001_inbox`), body unchanged (spec message-intake: Metadata extraction, Well-formed metadata fields; database-schema: Inbox storage); verify with table tests per field, repository and integration tests against the database, and a simulator run where every entry has all four fields

## Workflow follow-up

- Review with `/opsx:verify`, then archive with `/opsx:archive` before the next iteration.
