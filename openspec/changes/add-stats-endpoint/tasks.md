# Tasks

## 1. Read query

- [x] 1.1 Add `repository.Stats` in `internal/repository` implementing a `StatsRepository` interface declared in `internal/handler` (design D4). In one `REPEATABLE READ READ ONLY` transaction (D2) it returns the database `now()`, inbox totals by outcome with pending, the oldest pending entry's `received_at` and age rounded to milliseconds (D3), and per channel with identity: highest message number, count of entries numbered 1..highest, pending and outcome counts, for every channel without filtering. Errors go through `classify`. Verify with integration tests: empty inbox; mixed outcomes including an entry without identity and one with message number 0; a gap (1, 2, 5); a redelivery stored once; oldest pending age with a `received_at` set in the past; a report taken while another transaction holds `FOR UPDATE` on pending rows returns without waiting; a closed pool gives `ErrUnavailable`. Run `make test-integration`.

## 2. HTTP endpoint

- [x] 2.1 Add `handler.Stats` serving `GET /stats`: 5 s `readTimeout`, filters channels with `domain.ValidChannel`, orders by channel byte-wise, computes `missing` and `complete`, renders the D5 shape with `jsonTime` (`oldestPendingAt` `null` and age `0` when nothing is pending, `rockets` an empty array, never `null`), and answers failures through `readFailed`. Verify with unit tests against a fake repository under `go test -race ./internal/handler`: empty report, the gap/quarantined/pending/failed `complete` cases from the spec, an invalid channel counted but not listed, unsorted fake input returned sorted, `503` for `ErrUnavailable` and a timeout, `500` otherwise, a cancelled client request logged at debug only.
- [x] 2.2 Register `GET /stats` with the `methodNotAllowed("GET, HEAD")` fallback in `internal/server.Routes` and wire `repository.NewStats(pool)` in `cmd/rockets/server.go`. Verify with server tests: `GET` and `HEAD` reach the handler, `POST /stats` → `405` with `Allow: GET, HEAD`, `GET /stats?x=1` → `200`. Add an integration test through the real server and database that stores messages via `POST /messages`, runs the processor until drained, and checks `/stats` against the inbox. Run `make test` and `make test-integration`.

## 3. Deployment and docs

- [x] 3.1 Add `location /stats` to `deploy/nginx/default.conf` with `proxy_next_upstream error timeout http_502` (D6). Extend the existing paused-database E2E test so `GET /stats` also gets a single `503` within 9 s. Run `make test-e2e`.
- [x] 3.2 Document `/stats` in `README.md`: fields and their meaning (`highestMessageNumber` vs the rocket API's `lastMessageNumber`), `a polling loop (`watch -n1 curl -s localhost:8088/stats` where available) during a simulator run, that quarantined messages do not make a rocket incomplete, and the limitations from the design (full inbox scan per request, unpaged `rockets`, a lost trailing message is invisible). Verify the documented commands against the compose stack.

## 4. Integration

- [x] 4.1 Extend the simulator E2E test: after the default run and once drained, `/stats` reports `received` equal to the inbox count, `pending` 0, `failed` 0, every rocket `complete` with `missing` 0, and the same body (apart from `asOf` and the age) from repeated requests through nginx. Log the request time of `/stats` to check the D1 cost assumption. Run `make test`, `make test-integration`, `make lint` and `make test-e2e`, and record remaining limitations.

## Workflow follow-up

- Review the specification before starting implementation, then verify and archive this change.
