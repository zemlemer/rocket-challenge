# Spec Delta

## ADDED Requirements

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
