## MODIFIED Requirements

### Requirement: Processor graceful shutdown
On `SIGINT` or `SIGTERM` the processor SHALL stop starting new batches. One shutdown budget, the shutdown timeout counted from the signal, SHALL cover letting the batch in progress commit or roll back and closing the database pool. The rollback of a batch SHALL be attempted even when the batch's own timeout has expired, and SHALL be bounded by a short timeout of its own. If the batch has committed or rolled back and the pool is closed within the budget, the processor SHALL exit with status 0.

#### Scenario: Signal during a batch
- **WHEN** `SIGTERM` arrives while a batch is being processed
- **THEN** that batch is either fully committed or fully rolled back and the process exits with status 0

#### Scenario: Network hangs during rollback
- **WHEN** `SIGTERM` arrives while a batch fails and the connection to PostgreSQL hangs while its `ROLLBACK` is sent, and stays hung until the process exits
- **THEN** the process exits within the shutdown budget, and after the connection is restored the entries of that batch are applied exactly once

## ADDED Requirements

### Requirement: Processor unclean shutdown
If the batch in progress or the closing of the database pool has not finished when the shutdown budget is used up, the processor SHALL exit with status 1 and log that the shutdown was not clean. A pool close that still blocks SHALL NOT delay the exit.

#### Scenario: Pool close blocks
- **WHEN** the processing loop has returned after the signal but closing the database pool does not return within the rest of the budget
- **THEN** the process exits with status 1 when the budget is used up and logs that the shutdown was not clean
