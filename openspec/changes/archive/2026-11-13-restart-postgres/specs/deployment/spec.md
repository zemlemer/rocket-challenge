## MODIFIED Requirements

### Requirement: PostgreSQL service
The docker compose file SHALL start PostgreSQL 16 with a named volume, so data survives container re-creation, and a health check reporting readiness. Its port SHALL be published on the host for local tests. The service SHALL restart automatically after a crash and when the Docker daemon starts again, unless it was stopped explicitly.

#### Scenario: Data survives re-creation
- **WHEN** the stack is migrated, then `docker compose down` (without `-v`) and `docker compose up` are run
- **THEN** the schema and its data are still present

#### Scenario: Database crash
- **WHEN** the PostgreSQL server process exits abnormally while the stack is running (simulated with `pg_ctl stop -m immediate` inside the container; `docker kill` counts as an explicit stop and is not restarted)
- **THEN** it is restarted automatically, becomes healthy again, and `GET /stats` through `localhost:8088` answers `200` without any manual command
