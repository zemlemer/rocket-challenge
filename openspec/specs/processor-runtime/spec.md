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
On `SIGINT` or `SIGTERM` the processor SHALL stop starting new batches. One shutdown budget, the shutdown timeout counted from the signal, SHALL cover letting the batch in progress commit or roll back and closing the database pool. The rollback of a batch SHALL be attempted even when the batch's own timeout has expired, and SHALL be bounded by a short timeout of its own. If the batch has committed or rolled back and the pool is closed within the budget, the processor SHALL exit with status 0.

#### Scenario: Signal during a batch
- **WHEN** `SIGTERM` arrives while a batch is being processed
- **THEN** that batch is either fully committed or fully rolled back and the process exits with status 0

#### Scenario: Network hangs during rollback
- **WHEN** `SIGTERM` arrives while a batch fails and the connection to PostgreSQL hangs while its `ROLLBACK` is sent, and stays hung until the process exits
- **THEN** the process exits within the shutdown budget, and after the connection is restored the entries of that batch are applied exactly once

### Requirement: Processor unclean shutdown
If the batch in progress or the closing of the database pool has not finished when the shutdown budget is used up, the processor SHALL exit with status 1 and log that the shutdown was not clean. A pool close that still blocks SHALL NOT delay the exit.

#### Scenario: Pool close blocks
- **WHEN** the processing loop has returned after the signal but closing the database pool does not return within the rest of the budget
- **THEN** the process exits with status 1 when the budget is used up and logs that the shutdown was not clean
