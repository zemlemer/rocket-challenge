# Proposal

## Why

The unclean branch of the server shutdown is not bounded by `--shutdown-timeout`. An inbox insert runs on a context detached from the request (`context.WithoutCancel(r.Context())` plus a 10 s save timeout), so when the shutdown timeout expires `http.Server.Close` closes the connections but the insert keeps running. `ServerCmd.Run` then defers `pool.Close()`, which waits until every acquired connection is released. With an insert blocked in PostgreSQL (a lock, an overloaded database) the process exits only when the save timeout ends, and nothing at all bounds the wait for `pool.Close()` itself. An orchestrator that sends `SIGKILL` after its own grace period sees a server that ignores the configured timeout.

## What Changes

- Inserts get their own context with the lifetime of the server instead of a context detached from everything. It ends only when the server gives up on in-flight work; client disconnects still do not cancel an insert.
- After `SIGINT`/`SIGTERM` in-flight requests, and the inserts they run, keep the full `--shutdown-timeout` to finish, as today.
- When the timeout expires the server cancels the running inserts, closes the HTTP connections and exits with status 1.
- In that unclean branch the wait for `pool.Close()` is bounded by the same `--shutdown-timeout`; if the pool is still not closed the process exits with status 1 anyway and logs it.
- The clean branch is unchanged: all requests finished, pool closed, status 0.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `http-server`: requirement "Graceful shutdown" gains the cancellation of inserts and the bound on closing the pool.

## Impact

- Code: `cmd/rockets/server.go` (server-lifetime context, bounded pool close), `internal/server/server.go` (abort hook called on timeout, unclean-shutdown error), `internal/handler/messages.go` (insert context derived from the server lifetime).
- Tests: new e2e-style test in `cmd/rockets` holding a PostgreSQL lock until the process exits; unit test for the bounded close.
- No schema, SQL, HTTP API or deployment change. Compose keeps its default stop grace period (10 s) while the server default timeout stays 15 s; see design Risks.
