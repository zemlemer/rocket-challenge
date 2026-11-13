# processor-runtime Specification

## Purpose
Defines the `processor` mode of the `rockets` executable: how it starts, how it refuses an unmigrated database and how it stops.

## Requirements

### Requirement: Processor mode
`rockets processor` SHALL run the inbox processing loop with the executable's shared options only. Batch size (1,000 entries per transaction), poll interval for an empty inbox (100 ms) and shutdown timeout (15 s) SHALL be fixed in the code. The processor SHALL serve no HTTP endpoint.

#### Scenario: Start
- **WHEN** `rockets processor` is started against a migrated database
- **THEN** it logs that it started and processes unprocessed inbox entries

### Requirement: Processor schema check
`rockets processor` SHALL NOT migrate the database. On startup it SHALL check that the schema is at least at the version it requires and otherwise exit with status 1 and a message telling the operator to run `rockets migrate`.

#### Scenario: Unmigrated database
- **WHEN** `rockets processor` starts against a database without the processing schema
- **THEN** it exits with status 1 and a message naming `rockets migrate`

### Requirement: Processor graceful shutdown
On `SIGINT` or `SIGTERM` the processor SHALL stop starting new batches, let the batch in progress commit or roll back, close the database pool and exit with status 0 within the shutdown timeout; if the timeout expires it SHALL exit with status 1.

#### Scenario: Signal during a batch
- **WHEN** `SIGTERM` arrives while a batch is being processed
- **THEN** that batch is either fully committed or fully rolled back and the process exits with status 0
