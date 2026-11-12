# database-schema Specification

## Purpose

Defines how the PostgreSQL schema of the rockets system is versioned and migrated, so that every later feature can add its tables safely as numbered migrations.

## Requirements

### Requirement: Versioned migrations
The schema SHALL be defined by an ordered set of numbered, forward-only SQL migrations embedded in the `rockets` executable. Migrating SHALL apply, in order, every migration not yet applied to the database and record each applied version in the database.

#### Scenario: Empty database
- **WHEN** migrations are run against an empty database
- **THEN** every migration is applied in version order and each version is recorded as applied

#### Scenario: New migration added
- **WHEN** a database is at version 1 and the code contains migrations 1 and 2
- **THEN** only migration 2 is applied and existing data is kept

### Requirement: Repeatable migration
Running migrations against a database that is already at the latest version SHALL change nothing and SHALL succeed.

#### Scenario: Second run
- **WHEN** migrations are run twice against the same database
- **THEN** the second run applies nothing, succeeds, and leaves data unchanged

### Requirement: Concurrent migration safety
Several processes migrating the same database at the same time SHALL each succeed, and each migration SHALL be applied exactly once.

#### Scenario: Three processes start together
- **WHEN** three migration runs start concurrently against an empty database
- **THEN** all three succeed and every migration is recorded exactly once

### Requirement: Atomic migration steps
Each migration SHALL be applied atomically together with the record of its version: a failing migration SHALL leave neither partial schema changes nor a version record, and SHALL make the run fail with an error naming the migration.

#### Scenario: Broken migration
- **WHEN** a migration contains an invalid SQL statement
- **THEN** the run fails with an error naming that migration, the database stays at the previous version, and earlier migrations remain applied

### Requirement: Inbox storage
The schema SHALL provide an inbox that stores, per accepted request: the raw body as bytes; the parsed metadata — identity (`channel`, `messageNumber`), `messageTime` as a timestamp with time zone, `messageType` — each field when known or nothing; whether the body was truncated; and the time of receipt. Each entry SHALL get a unique, increasing id reflecting insertion order.

#### Scenario: Entry with identity
- **WHEN** a body is stored with channel `c1`, message number 5, a message time, message type `RocketLaunched` and truncation flag `false`
- **THEN** the inbox holds one entry with that body, metadata, flag and a receipt time

#### Scenario: Entry without identity
- **WHEN** a body that is not valid JSON is stored without identity
- **THEN** the inbox holds it with empty metadata

### Requirement: Inbox identity uniqueness
The inbox SHALL hold at most one entry per identity (`channel`, `messageNumber`). Entries without identity SHALL NOT be subject to this constraint.

#### Scenario: Same identity twice
- **WHEN** two entries with channel `c1` and message number 5 are stored
- **THEN** the second is refused by the database and the inbox holds only the first

#### Scenario: Two entries without identity
- **WHEN** two identical bodies are stored without identity
- **THEN** the inbox holds both
