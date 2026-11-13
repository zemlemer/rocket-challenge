# deployment Specification

## Purpose

Defines how the rockets system is packaged as a container image and started locally with docker compose together with its PostgreSQL database.

## Requirements

### Requirement: Container image
The repository SHALL provide a Dockerfile that builds one image containing the `rockets` executable, running as a non-root user, with `rockets` as the entrypoint so that the mode is chosen by the container command.

#### Scenario: Run a mode from the image
- **WHEN** the image is run with the command `migrate --help`
- **THEN** it prints the help of the `migrate` mode and exits with status 0

### Requirement: PostgreSQL service
The docker compose file SHALL start PostgreSQL 16 with a named volume, so data survives container re-creation, and a health check reporting readiness. Its port SHALL be published on the host for local tests.

#### Scenario: Data survives re-creation
- **WHEN** the stack is migrated, then `docker compose down` (without `-v`) and `docker compose up` are run
- **THEN** the schema and its data are still present

### Requirement: Migration service
The docker compose file SHALL define a one-shot `migrate` service running `rockets migrate` that starts only after PostgreSQL is healthy and exits when done. Services that need the schema SHALL start only after `migrate` has completed successfully.

#### Scenario: Stack start
- **WHEN** `docker compose up` is run on a machine without existing volumes
- **THEN** PostgreSQL becomes healthy, `migrate` applies all migrations and exits with status 0

#### Scenario: Repeated start
- **WHEN** `docker compose up` is run again on the same volume
- **THEN** `migrate` reports the schema is up to date and exits with status 0

### Requirement: Server replicas
The docker compose stack SHALL run three `rockets server` instances. They SHALL start only after PostgreSQL reports healthy and the `migrate` service has completed successfully, SHALL NOT publish ports on the host, and SHALL restart automatically after a crash.

#### Scenario: Stack start
- **WHEN** `docker compose up` is run on a fresh volume
- **THEN** PostgreSQL becomes healthy and `migrate` completes before three server instances start, and each answers `200` on `/healthz`

#### Scenario: Instance crash
- **WHEN** a server process is killed
- **THEN** its container is restarted automatically

### Requirement: Load-balanced entry point
The stack SHALL expose a single entry point on host port 8088 that distributes requests round-robin across the server instances. When an instance fails to respond, the entry point SHALL retry the request on another instance, including `POST /messages`, which is safe because intake does not store an identified message twice. A restarted instance SHALL rejoin the rotation without restarting the entry point.

#### Scenario: Requests spread over instances
- **WHEN** 30 requests are sent to `localhost:8088`
- **THEN** each of the three instances serves some of them

#### Scenario: Instance stopped during delivery
- **WHEN** one of three server instances is stopped while the simulator is sending messages
- **THEN** the simulator keeps receiving `2xx` responses and every message it sent is in the inbox

### Requirement: Entry point connection reuse
The entry point SHALL reuse connections to the server instances across requests instead of opening one per request, and SHALL close an idle connection to an instance before the instance's own idle timeout (60 s) would, so that it never sends a request on a connection the instance is closing.

#### Scenario: Burst of requests
- **WHEN** 200 requests are sent to `localhost:8088` one after another
- **THEN** the entry point opens far fewer than 200 connections to the server instances

### Requirement: Stop grace period
The stack SHALL give a stopping server container more time than the server's shutdown timeout before it is killed, so that a graceful shutdown is never cut short by the container runtime.

#### Scenario: Stop during a request
- **WHEN** `docker compose stop server` is run while a request is in flight
- **THEN** the container is not killed before the server's shutdown timeout has passed

### Requirement: No size limit at the entry point
The entry point SHALL forward request bodies of any size to the server instances, so that size handling is decided only by intake (see Oversized bodies) and never answered with `413` by the load balancer.

#### Scenario: 2 MiB body through the load balancer
- **WHEN** a client posts a 2 MiB body to `localhost:8088/messages`
- **THEN** the response is `202` and the inbox holds the truncated body

### Requirement: Simulator against the stack
With the stack running, the provided simulator run on the host with `./rockets launch "http://localhost:8088/messages"` and its default options SHALL receive only `2xx` responses.

#### Scenario: Default simulator run
- **WHEN** the simulator runs with default options against the stack
- **THEN** every request is answered with `2xx` and the inbox holds one entry per distinct message identity the simulator sent

### Requirement: Processor replicas
The docker compose stack SHALL run four `rockets processor` instances that start only after PostgreSQL is healthy and `migrate` has completed successfully, publish no ports on the host, restart automatically after a crash, and get a stop grace period longer than their shutdown timeout.

#### Scenario: Stack start
- **WHEN** `docker compose up` is run on a fresh volume
- **THEN** four processor instances start after `migrate` and log that they started

#### Scenario: One processor stopped
- **WHEN** one of the four processors is stopped while the simulator is sending messages
- **THEN** the others keep processing and every message is applied exactly once

### Requirement: Read failures are not retried
The entry point SHALL NOT retry a `503` response to a read request on `/rockets` paths or `/stats` on another instance, because a slow or unavailable database affects every instance alike. It SHALL still retry such a request on another instance when the connection to an instance fails.

#### Scenario: Database paused during a read
- **WHEN** the database is paused and a client sends `GET /rockets` through `localhost:8088`
- **THEN** the client gets `503` after one read timeout (about 5 s), not after one timeout per instance

#### Scenario: Database paused during a stats read
- **WHEN** the database is paused and a client sends `GET /stats` through `localhost:8088`
- **THEN** the client gets `503` after one read timeout (about 5 s), not after one timeout per instance

#### Scenario: Intake right after a failed read
- **WHEN** the database answers again right after that read
- **THEN** `POST /messages` requests through `localhost:8088` get `202` on every instance
