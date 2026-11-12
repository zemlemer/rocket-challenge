# Spec Delta

## ADDED Requirements

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
