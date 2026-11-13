## MODIFIED Requirements

### Requirement: Graceful shutdown
On `SIGINT` or `SIGTERM` the server SHALL stop accepting connections, let in-flight requests and the inbox inserts they run finish within a shutdown timeout (`SHUTDOWN_TIMEOUT` / `--shutdown-timeout`, default 15 s), close the database pool and exit with status 0. Inbox inserts SHALL run on a context with the lifetime of the server, so neither a client disconnect nor the signal itself cancels them. If requests are still in flight when the timeout expires, the server SHALL cancel the running inserts, close the HTTP connections, wait for the database pool to close for at most the same shutdown timeout, and exit with status 1 and a message saying the shutdown was not clean.

#### Scenario: Shutdown during a request
- **WHEN** `SIGTERM` is received while a `POST /messages` is in flight
- **THEN** that request completes with its normal response before the process exits with status 0

#### Scenario: Shutdown timeout exceeded
- **WHEN** `SIGTERM` is received while a request is in flight that does not finish within the shutdown timeout
- **THEN** the process exits with status 1 after the timeout and logs that the shutdown was not clean

#### Scenario: Insert blocked in the database
- **WHEN** `SIGTERM` is received while a `POST /messages` insert waits for a lock held in PostgreSQL until the process exits, with a shutdown timeout shorter than the insert's own timeout
- **THEN** the process exits with status 1 within twice the shutdown timeout, and the client gets no `202`
