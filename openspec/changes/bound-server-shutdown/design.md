# Design

## Context

Observed in the code before this change:

- `handler.Messages.ServeHTTP` reads the body, then saves with `context.WithTimeout(context.WithoutCancel(r.Context()), saveTimeout)` (`saveTimeout` = 10 s). The detachment is deliberate: a client that disconnects after sending the whole body must not leave a half-done save behind; identity dedup makes finishing harmless.
- `server.Run` calls `srv.Shutdown` with `--shutdown-timeout`; on expiry it calls `srv.Close()` and returns "shutdown was not clean". `srv.Close()` closes listeners and connections but does not wait for handlers, so a handler blocked in `Save` keeps running.
- `ServerCmd.Run` has `defer pool.Close()`. pgxpool's `Close` waits until all acquired connections are released, so it waits for that handler.

## Goals / Non-Goals

**Goals:**
- From `SIGTERM` to exit takes at most about twice `--shutdown-timeout`, whatever PostgreSQL does.
- In-flight inserts keep the whole timeout to finish; they are cancelled only when it expires.
- Client disconnects still never cancel an insert.

**Non-Goals:**
- No change to the processor's shutdown (it has the same `defer pool.Close()` pattern; noted under Risks).
- No change to the default timeout or to compose `stop_grace_period`.

## Decisions

### D1. One server-lifetime context for inserts

`ServerCmd` creates `work, abort := context.WithCancel(context.WithoutCancel(ctx))` and hands `work` to `handler.Messages` (new field `Work`). The handler derives the insert context from it: `context.WithTimeout(h.Work, saveTimeout)`. `work` is not cancelled by the signal (`WithoutCancel`), only by `abort`. A nil `Work` means `context.Background()`, so handler unit tests stay as they are.

*Alternative:* `http.Server.BaseContext` — rejected: the request context is cancelled on client disconnect, and the handler would still have to strip that with `WithoutCancel`, which also strips the base. *Alternative:* cancel through `context.AfterFunc` on the request context — same effect with more code; request values are not used by the save.

### D2. `server.Run` calls an abort hook on timeout

`server.Run` gets an `abort func()` argument (nil allowed). When `srv.Shutdown` returns because the timeout expired, it calls `abort()` first (cancel the inserts), then `srv.Close()` (close the HTTP connections), and returns an error wrapping the exported `server.ErrUncleanShutdown`. The server package still knows nothing about storage; it only signals "give up on in-flight work".

### D3. Bounded pool close in the unclean branch

`ServerCmd.Run` closes the pool in a deferred function. On `server.ErrUncleanShutdown` it runs `pool.Close()` in a goroutine and waits at most `--shutdown-timeout` (helper `closeWithin(close func(), d time.Duration) bool`). If the pool is not closed by then it logs a warning and returns, so the process exits with status 1; exiting closes the remaining sockets. In every other case `pool.Close()` runs unbounded, as before: no insert is in flight then (clean shutdown) or the server never started serving.

Cancelling the insert context makes pgx (v5.13, default `DeadlineContextWatcherHandler`) set an immediate deadline on the socket, so the query returns with an error, the connection is closed and released, and `pool.Close()` returns quickly after `abort()`. The bound covers anything that still blocks (for example closing idle connections to an unreachable host).

*Alternative:* a fixed short bound (e.g. 1 s) — rejected, a second knob with no clear value; reusing the configured timeout keeps one setting.

## Risks / Trade-offs

- [Worst case is about 2 × timeout] The shutdown can take the timeout for requests plus the timeout for the pool. → Accepted: the second half applies only when cancellation itself does not work.
- [Compose grace period] Docker sends `SIGKILL` after 10 s by default, before the 15 s server default. Unchanged by this change; an aborted request gets no `202` either way and the sender redelivers.
- [Cancelled on the client side only] pgx does not send a cancel request; it drops the connection. A backend waiting for a lock does not notice, so the insert may still commit once the lock is released. → Accepted: no `202` was sent, the sender redelivers, and the identity index turns the redelivery into a no-op. Switching the pool to `CancelRequestContextWatcherHandler` would abort the backend too but changes connection handling for every query; not done here.
- [Response after cancellation] An insert cancelled by `abort` makes the handler try to write a 5xx on a connection that `srv.Close()` closes; the client sees a closed connection and no `202`. No acknowledgement of an unstored message is possible.
- [Processor] `ProcessorCmd` has the same unbounded `defer pool.Close()` after its own timeout. Left for a separate change.

## Verification

- Verified by test: a PostgreSQL lock held by the test blocks an insert; after `SIGTERM` with `--shutdown-timeout 1s` the process exits with status 1 in well under the 10 s save timeout while the lock is still held, and the client gets no `202`. The lock is released only after the process has exited.
- Verified by unit test: `closeWithin` returns false after the bound when the close function blocks, true when it returns.
- Not tested: a `pool.Close()` that blocks for real; the bound in D3 is covered by the `closeWithin` unit test only.
