# Proposal

## Why

The processor can outlive its 15 s shutdown timeout when the connection to PostgreSQL hangs. The batch transaction is rolled back with `context.WithoutCancel(ctx)`, a context without any deadline, so a `ROLLBACK` sent into a hung connection waits until TCP keepalive gives up (about a minute). After the timeout `ProcessorCmd.Run` returns, but its deferred `pool.Close()` waits for the connection that the batch still holds. Reproduced through a TCP proxy: 16 s after `SIGTERM` the process was still running.

## What Changes

- The batch's `ROLLBACK` runs with its own short timeout (2 s), detached from the batch context, so the attempt can finish even after the batch context has expired and can never block for longer.
- After `SIGINT`/`SIGTERM` one budget of 15 s covers both finishing the batch in progress and closing the pool. The pool close gets whatever is left of it (reusing `closeWithin`).
- When the budget is used up the processor exits with status 1 and logs `shutdown was not clean`; a pool close that still blocks does not delay the exit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `processor-runtime`: requirement "Processor graceful shutdown" states the single budget for the batch and the pool close; new requirement "Processor unclean shutdown" covers the exhausted budget and the hung connection.

## Impact

- Code: `internal/repository/processing/processing.go` (rollback timeout), `cmd/rockets/processor.go` (shutdown budget, bounded pool close).
- Tests: `internal/testdb/proxy.go` gains a freeze triggered by a pattern in the client's traffic; new test in `cmd/rockets/processor_test.go` with the network hung during `ROLLBACK`; unit test of the exhausted-budget branch with a blocking pool close.
- No schema, SQL, HTTP API or deployment change. The compose stop grace period of the processors already exceeds 15 s.
