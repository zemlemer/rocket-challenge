# Proposal

## Why

Senders deliver rocket messages at least once and redeliver until they get a `2xx`. Before anything can be computed from those messages they must be accepted without loss: durably stored, never rejected, and acknowledged only once stored. This iteration delivers that front door — a stateless HTTP server that stores every request in an inbox — running as several instances behind a load balancer, so the provided simulator can already post to `localhost:8088` and every message it sends ends up in the database.

## What Changes

- Migration `0001_inbox`: the inbox table (raw body, optional identity `channel` + `messageNumber`, truncation flag, receipt time, insertion order) with identity uniqueness.
- New mode `rockets server`:
  - `POST /messages` stores the request body in the inbox and answers `202 Accepted`, or `5xx` if it could not be stored; never `4xx`, content is not validated. The identity is extracted leniently when present; a redelivery still waiting in the inbox is not stored twice. Bodies over 1 MiB are stored truncated.
  - `GET /healthz` is a liveness check (no database access); servers start only after the database is healthy.
  - Holds no state between requests; refuses to start on a database that has not been migrated; shuts down gracefully.
- Deployment: three `server` replicas started after `migrate`, behind nginx round-robin on host port 8088; nginx retries a failed request, including `POST`, on another replica.
- A first run of the provided simulator against the stack; the stored bodies are used to record the real message format in `docs/simulator-notes.md`.

Not in this iteration: message validation and quarantine, processing, rocket state, query endpoints — they come with change `add-rocket-state-tracking`.

## Capabilities

### New Capabilities

- `message-intake`: the `POST /messages` contract — response codes, durable storage, deduplication at intake, oversized bodies, outage recovery, concurrent intake, independence from processing.
- `http-server`: the `server` mode — listen address, schema version check, health endpoint, JSON errors for unknown routes, graceful shutdown, statelessness.

### Modified Capabilities

- `database-schema`: adds the inbox storage contract and identity uniqueness.
- `application-cli`: adds the database connection pool bounds (size, connect timeout).
- `deployment`: adds server replicas, the load balancer, their start order, connection reuse from the load balancer and a stop grace period.

## Impact

- Layering server → handler → repository interface owned by the handler: new packages `internal/server`, `internal/handler` and `internal/repository` (implementation of `handler.InboxRepository`); `internal/postgres` stays database infrastructure and gains the schema version check and migration `0001_inbox.sql`; `cmd/rockets` gains the `server` subcommand.
- New runtime dependency: nginx (≥ 1.27.3) in compose; new file `deploy/nginx/default.conf`.
- Change `add-rocket-state-tracking` is reduced accordingly: it builds on the inbox and the server from here.
