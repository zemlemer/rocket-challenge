# http-server Specification

## Purpose

Defines the `server` mode of the `rockets` executable: how it listens, refuses an unmigrated database, reports health, answers unknown routes, shuts down, and why any instance can serve any request.

## Requirements

### Requirement: Server mode
`rockets server` SHALL serve the system's HTTP API. It SHALL accept a listen address via `HTTP_ADDR` / `--http-addr` (default `:8088`) in addition to the executable's shared options.

#### Scenario: Custom listen address
- **WHEN** `rockets server --http-addr :9000` is started against a migrated database
- **THEN** it serves the API on port 9000

### Requirement: Schema version check
`rockets server` SHALL NOT migrate the database. On startup it SHALL check that the schema is at least at the version it requires and otherwise exit with status 1 and a message telling the operator to run `rockets migrate`.

#### Scenario: Unmigrated database
- **WHEN** `rockets server` starts against a database where `rockets migrate` has not been run
- **THEN** it exits with status 1 and a message naming `rockets migrate`

### Requirement: Liveness endpoint
`GET /healthz` SHALL return `200` whenever the server process is able to answer HTTP requests. It SHALL NOT check the database: database availability is reported per request by `POST /messages` (`503`), and the start order against the database is ensured by the deployment.

#### Scenario: Server running
- **WHEN** a client sends `GET /healthz` to a running server
- **THEN** the server responds `200`

#### Scenario: Database down
- **WHEN** the database is unreachable and a client sends `GET /healthz`
- **THEN** the server still responds `200`

### Requirement: Error responses for unknown routes
Requests to unknown paths SHALL get `404` and unsupported methods on known paths `405`, each with a JSON body `{"error": "<description>"}` and `Content-Type: application/json`.

#### Scenario: Wrong method
- **WHEN** a client sends `GET /messages`
- **THEN** the server responds `405` with an `error` field

#### Scenario: Unknown path
- **WHEN** a client sends `GET /nowhere`
- **THEN** the server responds `404` with an `error` field

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

### Requirement: Connection timeouts and reuse
The server SHALL bound every phase of a client connection — reading request headers (5 s), reading the whole request (30 s), writing the response (30 s), and keeping an idle connection open (60 s) — and SHALL keep connections open between requests (HTTP keep-alive) within the idle bound.

#### Scenario: Slow headers
- **WHEN** a client opens a connection and sends no complete request headers for more than 5 s
- **THEN** the server closes the connection

#### Scenario: Keep-alive
- **WHEN** a client sends two requests on the same connection
- **THEN** both are answered on that connection

### Requirement: Stateless instances
A server instance SHALL keep no state between requests other than its connection pool, so that several instances can serve the same database concurrently and any instance can serve any request.

#### Scenario: Two instances, one database
- **WHEN** two server instances store the same messages concurrently
- **THEN** the inbox holds exactly what one instance would have stored alone
