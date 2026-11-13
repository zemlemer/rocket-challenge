# Design

## Context

See proposal.md (Why). Current layout, observed in the code:

- `internal/repository` (about 500 lines without tests): `inbox.go`, `rockets.go`, `stats.go` implement `handler.InboxRepository`, `handler.RocketRepository`, `handler.StatsRepository`; `processing.go` implements `processor.Repository`.
- `helpers.go` holds `isUnavailable` (PostgreSQL/pgx error → "transient outage?") and two wrappers: `classify` (→ `handler.ErrUnavailable`) and `classifyProcessing` (→ `processor.ErrUnavailable`). `classify_internal_test.go` tests `isUnavailable` with a table.
- Test helpers in `helpers_test.go` are used as follows: `rows`, `id`, `withID`, `migratedDB` by the inbox and stats tests; `insertRocket`, `channels` by the rockets tests; `outcomes` by the processing tests; `drain` by both the processing tests and `TestRocketVisibilityThroughProcessing` in the rockets tests.
- Callers: `cmd/rockets/server.go`, `cmd/rockets/processor.go`, and `test/integration` (`helpers_test.go`, `intake_*_test.go`, `processing_test.go`, `query_test.go`, `stats_test.go`).
- The project convention (openspec/config.yaml) says helpers in `helpers.go`, test helpers in `helpers_test.go`, shared test helpers in `internal/testdb`.

## Goals / Non-Goals

**Goals:**
- Each adapter package imports exactly one port package: `intake` → `handler`, `processing` → `processor`.
- Keep one definition of the transient-error test.
- Make the diff a move: SQL, transactions, locking and error messages unchanged.

**Non-Goals:**
- No change to ports (interfaces in `handler` and `processor`), domain, schema or SQL.
- No new storage backend and no per-type packages (`inbox`, `rockets`, `stats` stay together).
- No new linter setup.

## Decisions

### D1. Two packages split by consumer, not four by type

`internal/repository/intake` (`Inbox`, `Rockets`, `Stats`) and `internal/repository/processing` (`Repository`). The boundary that matters is which port package a file may import; the three intake adapters share it, so splitting them further only adds import paths.
*Alternative:* one package per adapter — rejected, no dependency difference between `inbox`, `rockets`, `stats`.
*Alternative:* `internal/postgres/intake` etc. — rejected, `internal/postgres` is connections and migrations and is imported by `internal/testdb`; nesting adapters under it mixes infrastructure with port implementations.

### D2. `IsUnavailable` moves to `internal/postgres`

`isUnavailable` knows only PostgreSQL error codes and pgx/puddle/net error types; it has no port dependency. `internal/postgres` already owns PostgreSQL access and imports pgx, so it becomes `postgres.IsUnavailable(err error) bool` with the table test moved to `internal/postgres` unchanged (renamed to call the exported name). Each adapter package keeps a private `classify(op, err)` in its own `helpers.go` that wraps with its own `ErrUnavailable`; the generic `wrap` helper disappears because each package has only one target.
*Alternative:* `pkg/helpers`, per the "shared helpers go to pkg/helpers" convention — rejected: `pkg/helpers` is generic (backoff, JSON) and dependency-free apart from the standard library; putting pgx-specific classification there would pull pgx into every importer. This is a deliberate exception to the convention, recorded here.
*Alternative:* duplicate `isUnavailable` in both packages — rejected, the code list must not drift between intake and processing.

### D3. Names

`intake.NewInbox`, `intake.NewRockets`, `intake.NewStats` keep their names (`intake.Inbox` reads well). The processing adapter becomes `processing.Repository` with `processing.New(pool, log)`, avoiding `processing.NewProcessing`. The compile-time assertions (`var _ handler.X = ...`, `var _ processor.Repository = ...`) stay.

### D4. Test helpers

Package-local helpers move with their tests into each package's `helpers_test.go`. `drain` is needed by both packages, so it moves to `internal/testdb` as `testdb.Drain(t, repo, limit)`, taking a small interface (`ProcessBatch(ctx, int, func([]domain.InboxEntry) domain.BatchResult) (int, error)`) so `testdb` does not import the adapter. `testdb` already imports `domain`. `TestRocketVisibilityThroughProcessing` stays in `intake` and imports `processing` from its external test package; test-only imports do not affect the production boundary.

### D5. Guarding the boundary

A small unit test in each package (`imports_test.go`) parses the package's non-test files with `go/parser` (`ImportsOnly`) and fails if `intake` imports `rockets/internal/processor` or `processing` imports `rockets/internal/handler`. Without it the split only holds until someone adds the import back.
*Alternative:* golangci-lint `depguard` — rejected for now, the repository has no golangci config and adding one is outside this change.

## Risks / Trade-offs

- [Integration tests and `cmd` reference the old package] → They fail to compile, so nothing can be missed silently; all callers are listed in Context.
- [Exception to the pkg/helpers convention] → Recorded in D2; `postgres.IsUnavailable` is PostgreSQL-specific, not a generic helper.
- [More packages for a small codebase] → Two instead of one; accepted for the compiler-enforced boundary. Benefit is maintainability only; there is no runtime or deployment gain while there is one binary.
- [Verified vs. assumed] Behavior equivalence is assumed from the move being mechanical and verified by the existing unit, integration and e2e suites (integration tests already assert `handler.ErrUnavailable` for intake outages and `processor.ErrUnavailable` for processing outages).

## Migration Plan

Single commit-level refactor; no data or deployment migration. Rollback is a revert.
