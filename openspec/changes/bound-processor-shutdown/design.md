# Design

## Context

Observed in the code before this change:

- `processor.Run` runs each batch with `context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)` (10 s): the signal does not cancel a running batch, the batch timeout bounds it.
- `processing.Repository.process` defers `tx.Rollback(context.WithoutCancel(ctx))`. `WithoutCancel` also drops the batch deadline, so the rollback has no bound at all. If a statement fails with a server error (for example `lock_timeout`) the connection is still open and `ROLLBACK` is sent over it; a connection that hangs at that moment blocks the rollback until TCP keepalive closes it (`30 s + 3 × 10 s`).
- `ProcessorCmd.Run` waits for the loop for at most `processor.ShutdownTimeout` (15 s) and then returns "shutdown was not clean", but `defer pool.Close()` then waits for the connection the batch still holds: the process does not exit.

## Goals / Non-Goals

**Goals:**
- From `SIGINT`/`SIGTERM` to exit takes at most the 15 s budget, whatever the network or PostgreSQL does.
- The batch in progress keeps the batch timeout to finish; the rollback attempt is always made and is always bounded.
- Exit status 0 only if the batch has finished (committed or rolled back) and the pool is closed within the budget.

**Non-Goals:**
- No change to the batch timeout, the budget or the session limits.
- No change to the server shutdown.

## Decisions

### D1. Rollback with its own timeout

`process` defers the rollback with `context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)`, `rollbackTimeout = 2 s` (a constant of the processing package). Detaching keeps the attempt alive when the batch context has already expired; the timeout makes pgx (v5.13, default `DeadlineContextWatcherHandler`) set a deadline on the socket, so a hung `ROLLBACK` returns with an error and the connection is released to the pool, which destroys it. PostgreSQL rolls the transaction back when it notices the closed connection; nothing was committed either way.

pgx closes such a connection in the background (`PgConn.asyncClose`): it sends a cancel request over a new connection, sends `Terminate` and drains the socket, bounded by a fixed 15 s, and pgxpool's destructor waits for that. While the network stays hung, `pool.Close()` therefore blocks for up to 15 s; D2 bounds that wait.

A batch therefore holds a connection for at most `batchTimeout + rollbackTimeout` = 12 s, which fits the 15 s budget. In the one-by-one fallback each entry has its own rollback bound, but all entries share the batch context, so once it expires no new transaction begins.

*Alternative:* roll back with the batch context — rejected: when the batch timed out the context is already done and pgx would not even try, leaving the server-side transaction to `idle_in_transaction_session_timeout`. *Alternative:* a timeout equal to the remaining budget — rejected: the repository does not know about shutdown, and a normal rollback takes milliseconds.

### D2. One budget for the batch and the pool close

After the signal `ProcessorCmd.Run` hands over to `shutdown(done, closePool, budget, log)`: it fixes `deadline = now + budget`, waits for the loop until the deadline, then runs `closeWithin(closePool, time.Until(deadline))`. If the loop or the pool close is not done by the deadline it returns an error starting with `shutdown was not clean`, so `run` logs it and the process exits with status 1; `os.Exit` ends a pool close still running in the background. The deferred unbounded `pool.Close()` remains only for the paths before the signal (failed schema check, loop error).

`shutdown` takes plain functions and a channel, so the exhausted-budget branch can be tested with a pool close that blocks.

*Alternative:* the server's scheme of `timeout` for work plus `timeout` for the pool — rejected: the processor has a fixed, non-configurable timeout and the deployment grace period is derived from it; a single budget keeps "15 s" meaning 15 s.

## Risks / Trade-offs

- [Server-side transaction stays open while the network hangs] After the rollback timeout the client has given up, but a backend behind a hung network still holds the transaction and its row locks until it notices the connection is gone (keepalive or `idle_in_transaction_session_timeout`, 30 s). → Accepted: nothing is committed; other processors skip the locked inbox rows and wait at most `lock_timeout` for rocket rows. Same as any client crash.
- [Exit status 1 when the network hangs] The batch gives up its connection within the budget, but pgx's background close of that connection (see D1) keeps `pool.Close()` busy past the budget, so the processor exits with status 1. → Accepted: the status tells the operator the shutdown was not clean; nothing was committed and the batch is applied exactly once after recovery, which the test checks. A status 0 here would need closing pgx connections without its background cleanup.
- [Budget includes the batch] A batch that uses its full 10 s plus rollback leaves 3 s for the pool close. → Enough: closing idle connections only writes a `Terminate` message.

## Verification

- Integration test: a row lock held by the test makes the batch fail with `lock_timeout`; the test proxy freezes all traffic as soon as the processor sends `rollback` and keeps it frozen until the process has exited. After `SIGTERM` the process exits within the 15 s budget with status 1 and `shutdown was not clean: database pool still not closed` — so the loop returned in time and only the pool close blocked. With the old unbounded rollback the same test fails with `batch still running` (checked by reverting D1 locally). After the network is restored and the lock released, a second processor run applies the batch exactly once (state equals the reference fold of the inbox).
- Unit test: with the loop done and a pool close that blocks, `shutdown` returns `shutdown was not clean` after the budget; with a loop that does not finish, the same; with both done in time, nil.
- The integration test also exercises a real blocking `pool.Close()`; the unit test covers the branches exactly, independent of pgx internals.
