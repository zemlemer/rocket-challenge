# Spec Delta

## Purpose

Accepts messages posted by senders and durably stores them in an inbox for later processing, acknowledging only what has been stored, without inspecting or rejecting their content.

## ADDED Requirements

### Requirement: Message endpoint
The system SHALL accept messages via `POST /messages` and store each complete request body unchanged in the inbox together with the message metadata parsed from it (see Metadata extraction). Intake SHALL NOT evaluate the `message` payload and SHALL NOT update rocket state.

#### Scenario: Message stored
- **WHEN** a client posts a `RocketLaunched` message for channel `c1` with message number 1
- **THEN** the system responds `202 Accepted` after the body is stored in the inbox with channel `c1`, message number 1, its `messageTime` and its `messageType`

#### Scenario: Message without usable identity
- **WHEN** a client posts a JSON body whose `messageNumber` is `1.5`
- **THEN** the system responds `202` and the body is stored without identity

### Requirement: Metadata extraction
Intake SHALL parse the four `metadata` fields and store each one that is well-formed, independently of the others, and store nothing for a malformed one; a malformed field SHALL NOT cause a rejection. Invalid or truncated JSON and an empty body yield no metadata at all. The identity (`channel`, `messageNumber`) SHALL be stored only when both of its fields are well-formed.

#### Scenario: Full metadata
- **WHEN** a message has `channel` `"c1"`, `messageNumber` `7`, `messageTime` `"2022-02-02T19:39:05.86337+01:00"` and `messageType` `"RocketSpeedIncreased"`
- **THEN** the entry stores all four values, `messageTime` as an instant in time

#### Scenario: Malformed time, valid identity
- **WHEN** a message has a valid `channel` and `messageNumber` but `messageTime` `"yesterday"`
- **THEN** the system responds `202` and the entry stores the identity and `messageType` but no `messageTime`

### Requirement: Well-formed metadata fields
A metadata field SHALL be well-formed when: `channel` is a non-empty JSON string of at most 255 bytes without NUL; `messageNumber` is a JSON integer literal (no fraction, no exponent) between 1 and 2^63−1; `messageTime` is an RFC 3339 timestamp string with a time-zone offset; `messageType` is a non-empty JSON string of at most 255 bytes without NUL. Intake SHALL NOT check whether `messageType` is a known type.

#### Scenario: Unknown message type is kept
- **WHEN** a message has `messageType` `"RocketTeleported"`
- **THEN** the entry stores `RocketTeleported` as its message type

#### Scenario: NUL in channel
- **WHEN** a message has `channel` `"a\u0000b"`
- **THEN** the system responds `202` and the body is stored without identity

#### Scenario: Overlong channel
- **WHEN** a message has a 300-byte `channel`
- **THEN** the system responds `202` and the body is stored without identity

#### Scenario: Exponent notation
- **WHEN** a message has `messageNumber` `1e3`
- **THEN** the system responds `202` and the body is stored without identity

#### Scenario: Empty body
- **WHEN** a client posts an empty body
- **THEN** the system responds `202` and an empty entry without metadata is stored

### Requirement: Incomplete requests
A request whose body could not be read completely — the client disconnected or the read timed out — SHALL NOT be stored, neither in part nor as truncated, so that a later complete redelivery is not shadowed by a partial copy. If a response can still be sent it SHALL be a `5xx`.

#### Scenario: Client disconnects mid-body
- **WHEN** a client announces a 10 KiB body, sends 2 KiB and closes the connection
- **THEN** nothing is stored in the inbox

#### Scenario: Complete redelivery after an aborted attempt
- **WHEN** the aborted message is later delivered completely
- **THEN** it is stored with its identity and full body and answered with `202`

### Requirement: Response codes
`POST /messages` SHALL respond `202 Accepted` whenever the request has been durably stored, regardless of its content, and `5xx` only when it could not be stored. It SHALL NOT respond with `4xx`. Messages are never rejected because senders redeliver them unchanged: a rejection could never be resolved by redelivery.

#### Scenario: Content is not checked
- **WHEN** a client posts a body that is not valid JSON, or a message of an unknown type
- **THEN** the system responds `202` once the body is stored

#### Scenario: Storage failure
- **WHEN** the request cannot be stored because the database is unavailable
- **THEN** the system responds `503 Service Unavailable` and the sender is expected to redeliver

### Requirement: Durable acknowledgement
The system SHALL respond `202` only after the request is committed to the database. A request answered with `5xx` SHALL leave nothing stored.

#### Scenario: Acknowledged message survives a crash
- **WHEN** a message was answered with `202` and every process, including the database, is restarted
- **THEN** the message is still in the inbox after the restart

### Requirement: Duplicate deliveries at intake
A request whose identity (`channel`, `messageNumber`) is already waiting in the inbox SHALL be answered with `202` without storing a second copy; the first stored body wins. A redelivery of an already processed message is stored again like any new request and is discarded as a duplicate by processing.

#### Scenario: Redelivery while waiting
- **WHEN** the same message is delivered three times before it is processed
- **THEN** every delivery is answered with `202` and the inbox holds one copy

### Requirement: Oversized bodies
A body larger than 1 MiB (1,048,576 bytes) SHALL be stored truncated to its first 1 MiB, marked as truncated, and answered with `202`. A body of exactly 1 MiB SHALL be stored complete and not marked as truncated.

#### Scenario: 2 MiB body
- **WHEN** a client posts a 2 MiB body
- **THEN** the system responds `202` and the inbox holds its first 1 MiB marked as truncated

#### Scenario: Exactly 1 MiB
- **WHEN** a client posts a body of exactly 1,048,576 bytes
- **THEN** the inbox holds the whole body, not marked as truncated

### Requirement: Recovery from storage outage
After the database becomes reachable again, intake SHALL resume storing messages without a restart of the service.

#### Scenario: Database restarted
- **WHEN** the database is restarted while the service keeps running
- **THEN** requests answered with `503` during the outage are answered with `202` when redelivered after it

### Requirement: Concurrent intake
Concurrent requests, including requests served by different service instances, SHALL each be stored or deduplicated as if they had been handled one at a time.

#### Scenario: Same message to several instances
- **WHEN** 20 concurrent requests deliver the same message through different instances
- **THEN** all receive `202` and the inbox holds one copy

### Requirement: Independence from processing
Intake SHALL keep accepting messages while message processing is stopped or failing.

#### Scenario: Processor down
- **WHEN** no processor is running and messages are posted
- **THEN** they are answered with `202` and wait in the inbox until processing resumes
