# Tasks

## 1. Shared pieces

- [x] 1.1 Move `isUnavailable` and `unavailableCodes` from `internal/repository/helpers.go` to `internal/postgres` as exported `postgres.IsUnavailable` (design D2), and move `classify_internal_test.go` there as the table test `TestIsUnavailable` calling the exported name with the same cases. Point `classify`/`classifyProcessing` in `internal/repository` at `postgres.IsUnavailable` so the old package still builds. Verify with `go build ./...` and `go test -race ./internal/postgres ./internal/repository`.
- [x] 1.2 Add `testdb.Drain(t, repo, limit)` to `internal/testdb` over a minimal `ProcessBatch` interface (design D4) and replace the local `drain` in `internal/repository/helpers_test.go` with it. Verify with `make test-integration` (repository tests unchanged and green).

## 2. Processing adapter

- [x] 2.1 Create `internal/repository/processing`: move `processing.go` and `processing_test.go`, rename `Processing`/`NewProcessing` to `Repository`/`New` (design D3), add a private `classify` in its `helpers.go` that wraps with `processor.ErrUnavailable` via `postgres.IsUnavailable`, and move `outcomes` into its `helpers_test.go`. Update `cmd/rockets/processor.go`, `test/integration/processing_test.go` and `internal/repository/rockets_test.go` to the new package. Add `imports_test.go` failing if a non-test file imports `rockets/internal/handler` (design D5). Verify with `go build ./...`, `go vet ./...` and `make test-integration`.

## 3. Intake adapters

- [x] 3.1 Create `internal/repository/intake`: move `inbox.go`, `rockets.go`, `stats.go` with their tests, keep `classify` (wrapping `handler.ErrUnavailable`) in its `helpers.go`, drop `classifyProcessing` and `wrap`, and move the remaining test helpers into its `helpers_test.go`. Update `cmd/rockets/server.go` and `test/integration/{helpers,intake_identity,intake_outage,query,stats}_test.go`. Delete the now-empty `internal/repository` files. Add `imports_test.go` failing if a non-test file imports `rockets/internal/processor` (design D5). Verify with `go build ./...`, `go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/repository/...` (no `handler` in `processing`, no `processor` in `intake`), and `make test-integration`.

## 4. Integration

- [x] 4.1 Confirm the refactor changed no behavior: `git diff --stat -M` shows the SQL files as renames with only package/import/name edits; run `make lint`, `make test`, `make test-integration` and `make test-e2e` (including the simulator run when `ROCKETS_SIMULATOR` is set), and record any remaining limitations.

## Workflow follow-up

- Review the proposal and design before starting implementation, then verify and archive this change.
