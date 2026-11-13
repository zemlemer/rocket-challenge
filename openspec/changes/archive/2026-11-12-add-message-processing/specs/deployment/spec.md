# Spec Delta

## ADDED Requirements

### Requirement: Processor replicas
The docker compose stack SHALL run four `rockets processor` instances that start only after PostgreSQL is healthy and `migrate` has completed successfully, publish no ports on the host, restart automatically after a crash, and get a stop grace period longer than their shutdown timeout.

#### Scenario: Stack start
- **WHEN** `docker compose up` is run on a fresh volume
- **THEN** four processor instances start after `migrate` and log that they started

#### Scenario: One processor stopped
- **WHEN** one of the four processors is stopped while the simulator is sending messages
- **THEN** the others keep processing and every message is applied exactly once
