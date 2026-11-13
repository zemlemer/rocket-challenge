# Tasks

## 1. Bounded processor shutdown

- [ ] 1.1 Roll back the batch transaction with its own 2 s timeout detached from the batch context (design D1). Verify with `go test ./internal/repository/processing` against the test database.
- [ ] 1.2 Add `shutdown(done, closePool, budget, log)` to `cmd/rockets` and use it in `ProcessorCmd.Run` after the signal; keep the unbounded `pool.Close()` only for the paths before the signal (design D2). Unit-test the exhausted-budget branch with a blocking pool close.
- [ ] 1.3 Let `testdb.Proxy` freeze all traffic when the client sends a given pattern, until `Stop`. Add the test "Network hangs during rollback" in `cmd/rockets/processor_test.go`.
- [ ] 1.4 Run `make lint`, `make test`, `make test-integration` and record any remaining limitations.
