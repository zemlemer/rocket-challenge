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
