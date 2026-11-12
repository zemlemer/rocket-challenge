# application-cli Specification

## Purpose

Defines the single `rockets` executable: how its operating mode is selected, how it is configured, how it reports errors, and the behavior of its `migrate` mode.

## Requirements

### Requirement: Modes as subcommands
The system SHALL be shipped as one executable, `rockets`, whose operating mode is selected by its first argument (subcommand). `rockets --help` SHALL list the available modes; `rockets <mode> --help` SHALL describe the mode's options. A missing or unknown mode SHALL print usage to stderr and exit with status 2.

#### Scenario: Help
- **WHEN** `rockets --help` is run
- **THEN** it prints the available modes, including `migrate`, and exits with status 0

#### Scenario: Unknown mode
- **WHEN** `rockets launch` is run
- **THEN** it prints an error and usage to stderr and exits with status 2

### Requirement: Configuration from environment and flags
Every option SHALL be settable by an environment variable and overridable by a command-line flag. The database connection string SHALL be given by `DATABASE_URL` or `--database-url`; modes that need the database SHALL exit with status 2 and a message naming both when it is missing.

#### Scenario: Missing database URL
- **WHEN** `rockets migrate` is run without `DATABASE_URL` and without `--database-url`
- **THEN** it prints an error naming `DATABASE_URL` and `--database-url` and exits with status 2

#### Scenario: Flag overrides environment
- **WHEN** `DATABASE_URL` points to database A and `--database-url` to database B
- **THEN** database B is used

### Requirement: Database connection retry
A mode that needs the database SHALL retry the initial connection with backoff for up to a configurable timeout (`DB_CONNECT_TIMEOUT` / `--db-connect-timeout`, default 30 s) and exit with status 1 and a clear message if the database is still unreachable after it.

#### Scenario: Database comes up late
- **WHEN** `rockets migrate` starts 5 seconds before the database accepts connections
- **THEN** it connects once the database is up and completes normally

#### Scenario: Database never comes up
- **WHEN** the database stays unreachable for longer than the connect timeout
- **THEN** the command exits with status 1 and a message saying the database could not be reached

### Requirement: Migrate mode
`rockets migrate` SHALL bring the database schema to the latest version embedded in the binary (see database-schema), log each applied migration and the resulting version, and exit with status 0 on success and 1 on failure.

#### Scenario: Fresh database
- **WHEN** `rockets migrate` is run against an empty database
- **THEN** it creates the migration bookkeeping, logs each applied migration and the final schema version, and exits with status 0

#### Scenario: Already migrated
- **WHEN** `rockets migrate` is run against a database at the latest version
- **THEN** it logs that the schema is up to date and exits with status 0

#### Scenario: Failing migration
- **WHEN** a migration fails
- **THEN** the command logs the error naming the migration and exits with status 1

### Requirement: Structured logs
The executable SHALL write logs as structured lines to stderr with a level, a message and key-value attributes.

#### Scenario: Applied migration log
- **WHEN** a migration is applied
- **THEN** a log line at level INFO contains the migration version and name

### Requirement: Database connection pool
Modes that use the database SHALL keep a pool of reusable connections with a bounded size (default 10 connections per process) and a bounded time to establish a connection (default 5 s). Both SHALL be overridable through the database URL parameters `pool_max_conns` and `connect_timeout`.

#### Scenario: Defaults
- **WHEN** a mode connects with a database URL without pool parameters
- **THEN** the pool holds at most 10 connections and a connection attempt is abandoned after 5 s

#### Scenario: Override through the URL
- **WHEN** the database URL contains `pool_max_conns=3&connect_timeout=2`
- **THEN** the pool holds at most 3 connections and a connection attempt is abandoned after 2 s

#### Scenario: Unresponsive database host
- **WHEN** the database host accepts no TCP connection and drops packets silently
- **THEN** a request needing the database fails within the connect timeout instead of hanging
