# Proposal

## Why

`internal/repository` holds the adapters for two unrelated consumers: the HTTP layer (`Inbox`, `Rockets`, `Stats` implement ports from `internal/handler`) and the background processor (`Processing` implements `processor.Repository`). The package therefore imports both `handler` and `processor`, and nothing but review keeps processing code from using HTTP-layer types. The concrete risk is in error classification: `classify` wraps with `handler.ErrUnavailable` and `classifyProcessing` with `processor.ErrUnavailable`, and calling the wrong one compiles. Splitting the package lets the compiler enforce the boundary.

## What Changes

- `internal/repository` is replaced by two packages:
  - `internal/repository/intake`: `Inbox`, `Rockets`, `Stats`; imports `handler` and `domain`, never `processor`.
  - `internal/repository/processing`: `Processing`; imports `processor` and `domain`, never `handler`.
- The PostgreSQL error test `isUnavailable` (transient outage vs. bug) moves to `internal/postgres` as the exported `postgres.IsUnavailable`, together with its table test. Each new package wraps errors with its own `ErrUnavailable` only.
- Method sets and SQL stay the same. `intake` keeps the names `NewInbox`, `NewRockets`, `NewStats`; the processing adapter becomes `processing.Repository` / `processing.New` to avoid `processing.NewProcessing`. Callers in `cmd/rockets` and `test/integration` change their imports accordingly.
- Shared test helpers move with their tests; `drain` (needed by both packages' tests) moves to `internal/testdb`.
- No SQL, schema, HTTP, CLI or deployment change. Pure refactor.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. No requirement changes, so the change sets `skip_specs: true`.

## Impact

- Code: `internal/repository/**` (moved and split), `internal/postgres` (gains `IsUnavailable`), `internal/testdb` (gains `Drain`), `cmd/rockets/server.go`, `cmd/rockets/processor.go`, `test/integration/*` (import paths).
- Behavior: none. Verified by the unit, integration and e2e suites, which already assert `handler.ErrUnavailable` for intake outages and `processor.ErrUnavailable` for processing outages, and by checking each new package's import list.
- Docs: none reference `internal/repository`.
