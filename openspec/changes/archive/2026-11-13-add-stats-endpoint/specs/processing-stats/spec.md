# Spec Delta

## Purpose

Lets an operator or reviewer see, with one HTTP request, how far intake and processing have progressed across the whole deployment and whether every rocket has received and applied a complete, gap-free message sequence.

## ADDED Requirements

### Requirement: Stats endpoint
`GET /stats` SHALL respond `200` with a JSON object holding an `asOf` timestamp (the database time the report was taken, RFC 3339 UTC), an `inbox` object and a `rockets` array. `HEAD /stats` SHALL be accepted like `GET`. Other methods SHALL get `405` and query parameters SHALL be ignored.

#### Scenario: Empty system
- **WHEN** a client sends `GET /stats` against a migrated database with an empty inbox
- **THEN** the server responds `200` with all `inbox` counts `0`, `oldestPendingAt` `null`, `oldestPendingAgeSeconds` `0` and an empty `rockets` array

#### Scenario: Wrong method
- **WHEN** a client sends `POST /stats`
- **THEN** the server responds `405` with an `error` field

### Requirement: Inbox totals
The `inbox` object SHALL hold `received` (all inbox entries), `pending` (entries not processed yet), and `applied`, `quarantined` and `failed` (processed entries by outcome), so that `received` equals `pending + applied + quarantined + failed`. Entries without a parsed identity or with an invalid channel SHALL be counted here.

#### Scenario: Mixed outcomes
- **WHEN** the inbox holds 5 applied, 2 quarantined, 1 failed and 3 unprocessed entries, one of them without identity
- **THEN** `inbox` reports `received` 11, `pending` 3, `applied` 5, `quarantined` 2 and `failed` 1

### Requirement: Processing lag
The `inbox` object SHALL hold `oldestPendingAt`, the receive time of the oldest unprocessed entry, and `oldestPendingAgeSeconds`, its age at `asOf` in seconds with millisecond precision. With nothing pending they SHALL be `null` and `0`.

#### Scenario: Backlog
- **WHEN** the oldest unprocessed entry was received 2.5 s before the report is taken
- **THEN** `oldestPendingAgeSeconds` is `2.5` and `oldestPendingAt` is that entry's receive time

#### Scenario: Drained
- **WHEN** every inbox entry has been processed
- **THEN** `pending` is `0`, `oldestPendingAt` is `null` and `oldestPendingAgeSeconds` is `0`

### Requirement: Per-rocket completeness
`rockets` SHALL hold one object per valid channel with at least one inbox entry numbered 1 or higher, ordered by channel byte-wise, whether or not the rocket is launched. Each SHALL hold `channel`, `highestMessageNumber`, `received`, `missing`, `pending`, `applied`, `quarantined`, `failed` and `complete`, counting only that channel's entries numbered 1 or higher. Invalid channels SHALL NOT be listed.

#### Scenario: Rocket not launched yet
- **WHEN** channel `c1` has inbox entries for message numbers 2 and 3 only
- **THEN** `rockets` lists `c1` with `highestMessageNumber` 3, `received` 2 and `missing` 1

#### Scenario: Invalid channel
- **WHEN** an entry with channel `bad channel` is in the inbox
- **THEN** it is counted in `inbox` and not listed in `rockets`

### Requirement: Missing message numbers
For a rocket, `missing` SHALL be the number of message numbers from 1 to `highestMessageNumber` that have no inbox entry of that channel. A redelivered message SHALL NOT be counted twice.

#### Scenario: Gap
- **WHEN** channel `c1` has entries for message numbers 1, 2 and 5
- **THEN** its `missing` is 2

#### Scenario: Duplicate delivery
- **WHEN** message 2 of `c1` is delivered twice and messages 1 and 3 once
- **THEN** its `received` is 3 and `missing` is 0

### Requirement: Rocket complete flag
A rocket's `complete` SHALL be `true` exactly when its `missing`, `pending` and `failed` are all `0`. Quarantined entries SHALL NOT make a rocket incomplete.

#### Scenario: Complete with a quarantined message
- **WHEN** `c1` has entries 1 to 4, all processed, with one quarantined and none failed
- **THEN** its `complete` is `true`

#### Scenario: Pending message
- **WHEN** `c1` has entries 1 to 4 and entry 4 is not processed yet
- **THEN** its `complete` is `false` and `pending` is 1

### Requirement: Cluster-wide consistent report
The report SHALL be computed from the database alone, so that every server instance gives the same answer for the same stored data. The `inbox` totals and `rockets` entries of one response SHALL come from one consistent snapshot. Producing the report SHALL NOT block intake or processing.

#### Scenario: Any replica
- **WHEN** a client sends `GET /stats` repeatedly through the load balancer while nothing is being stored or processed
- **THEN** every response holds the same counts regardless of which instance answered

#### Scenario: During processing
- **WHEN** `GET /stats` is served while a processor commits a batch
- **THEN** the sum of `applied`, `quarantined` and `failed` over `rockets` never exceeds the matching `inbox` totals and the batch commits without waiting for the report

### Requirement: Stats availability
`GET /stats` SHALL respond `503` with an `error` field when the database is unavailable or does not answer within 5 s, and `500` on other failures. A request abandoned by its client SHALL NOT be logged as a server error.

#### Scenario: Database down
- **WHEN** the database is unreachable and a client sends `GET /stats`
- **THEN** the server responds `503` with an `error` field

#### Scenario: Client disconnects
- **WHEN** a client closes the connection while its `GET /stats` is being served
- **THEN** no error-level log entry is written
