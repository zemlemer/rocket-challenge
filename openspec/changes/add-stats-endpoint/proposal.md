# Proposal

## Why

A reviewer who runs the simulator against the compose stack can see that requests got `202` and can read rocket state, but cannot see whether the processors kept up, when the inbox was drained, or whether every rocket received every message. Today the only way is hand-written `psql` queries, and the completeness question (no gaps, no duplicates, nothing failed) has no query at all, although it is the most direct evidence for per-rocket ordering and idempotency.

## What Changes

- Add `GET /stats` (and `HEAD`) to `rockets server`: a read-only report computed from the database, so every replica behind nginx gives the same answer.
- The report contains:
  - inbox totals: entries received, pending, and processed by outcome (`applied`, `quarantined`, `failed`);
  - processing lag: the receive time and age of the oldest pending entry;
  - per rocket (every valid channel seen in the inbox): highest message number, entries received, missing message numbers, pending, outcomes, and whether the rocket is complete (nothing missing, pending or failed).
- Same failure behavior as the rocket read endpoints: `503` when the database is unavailable or slower than 5 s, `500` otherwise, no error log for a client that went away.
- README: how to watch the report during a simulator run (`watch -n1 curl -s localhost:8088/stats`).

Out of scope: Prometheus or other per-process metrics, a readiness endpoint, a processor health check, request access logs.

## Capabilities

### New Capabilities
- `processing-stats`: a cluster-wide, database-derived report on intake and processing progress and per-rocket message completeness.

### Modified Capabilities
- `deployment`: the entry point must not retry a `503` from `/stats` on another replica, as already required for `/rockets` (otherwise a database outage makes the client wait one read timeout per replica).

The new route follows the existing `http-server` rules for unknown routes and methods without changing them.

## Impact

- Code: new handler and repository read in `internal/handler` and `internal/repository`, one route in `internal/server`, wiring in `cmd/rockets/server.go`, a `location /stats` in `deploy/nginx/default.conf`.
- Database: read-only queries over `inbox`; no migration. Each request scans the inbox (see design), acceptable at challenge scale.
- API: one new public endpoint not named in the challenge; existing endpoints unchanged.
- Docs: README section for the endpoint.
