## MODIFIED Requirements

### Requirement: Processing lag
The `inbox` object SHALL hold `oldestPendingAt`, the earliest receive time among unprocessed entries, and `oldestPendingAgeSeconds`, its age at `asOf` in seconds with millisecond precision. The earliest receive time SHALL NOT depend on the order in which entries are stored. With nothing pending they SHALL be `null` and `0`.

#### Scenario: Backlog
- **WHEN** the oldest unprocessed entry was received 2.5 s before the report is taken
- **THEN** `oldestPendingAgeSeconds` is `2.5` and `oldestPendingAt` is that entry's receive time

#### Scenario: Storage order differs from receive order
- **WHEN** two unprocessed entries exist and the one stored later has the earlier receive time
- **THEN** `oldestPendingAt` is the earlier receive time

#### Scenario: Drained
- **WHEN** every inbox entry has been processed
- **THEN** `pending` is `0`, `oldestPendingAt` is `null` and `oldestPendingAgeSeconds` is `0`
