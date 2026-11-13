# message-intake Specification

## Purpose

Accepts messages posted by senders and durably stores them in an inbox for later processing, acknowledging only what has been stored, without inspecting or rejecting their content.

## Requirements

### Requirement: Message endpoint
The system SHALL accept messages via `POST /messages`. For a complete request body of at most 1 MiB, it SHALL store the body unchanged in the inbox with the parsed metadata (see Metadata extraction). Larger bodies SHALL follow Oversized bodies. Intake SHALL NOT evaluate the `message` payload or update rocket state.

#### Scenario: Message stored
- **WHEN** a client posts a `RocketLaunched` message for channel `c1` with message number 1
- **THEN** the system responds `202 Accepted` after the body is stored in the inbox with channel `c1`, message number 1, its `messageTime` and its `messageType`

#### Scenario: Message without usable identity
- **WHEN** a client posts a JSON body whose `messageNumber` is `1.5`
- **THEN** the system responds `202` and the body is stored without identity

### Requirement: Metadata extraction
For a complete body of at most 1 MiB, intake SHALL store each well-formed `metadata` field independently and SHALL leave each malformed field absent without rejecting the request. Invalid JSON, empty bodies and oversized bodies SHALL yield no metadata. Intake SHALL store the identity (`channel`, `messageNumber`) only when both of its fields are well-formed.

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
A request whose body cannot be read completely SHALL create no inbox entry, even if the bytes already received exceed 1 MiB. If a response can still be sent, it SHALL be a `5xx`.

#### Scenario: Client disconnects mid-body
- **WHEN** a client announces a 10 KiB body, sends 2 KiB and closes the connection
- **THEN** nothing is stored in the inbox

#### Scenario: Complete redelivery after an aborted attempt
- **WHEN** the aborted message is later delivered completely
- **THEN** it is stored with its identity and full body and answered with `202`

#### Scenario: Client disconnects after exceeding the limit
- **WHEN** a client announces a 2 MiB body, sends 1,048,577 bytes and closes the connection
- **THEN** nothing is stored in the inbox

#### Scenario: Read times out after exceeding the limit
- **WHEN** intake receives more than 1 MiB and the body read times out before the end of the request
- **THEN** nothing is stored in the inbox, and the system responds with `5xx` if it can still send a response

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
A request whose identity (`channel`, `messageNumber`) is already in the inbox — waiting or already processed — SHALL be answered with `202` without storing a second copy; the first stored body wins. Inbox entries are never deleted, so the inbox alone decides the answer to a sender: the response SHALL NOT depend on whether or how the message was processed.

#### Scenario: Redelivery while waiting
- **WHEN** the same message is delivered three times before it is processed
- **THEN** every delivery is answered with `202` and the inbox holds one copy

#### Scenario: Redelivery after processing
- **WHEN** a message is processed and then delivered again
- **THEN** the redelivery is answered with `202`, nothing new is stored, and rocket state is unchanged

#### Scenario: Redelivery of a quarantined message
- **WHEN** a message was processed with outcome `quarantined` and is delivered again
- **THEN** the redelivery is answered with `202` and the inbox still holds the single quarantined entry

### Requirement: Oversized bodies
After reading a complete body larger than 1 MiB (1,048,576 bytes), intake SHALL commit an inbox entry with the size flag set and zero body bytes, then respond `202`. The entry SHALL have no parsed metadata. A complete body of exactly 1 MiB SHALL be stored unchanged with the size flag clear.

#### Scenario: 2 MiB body
- **WHEN** a client delivers a complete 2 MiB body, even if the prefix contains well-formed metadata
- **THEN** the system responds `202` after committing an inbox entry with `truncated = true`, an empty body and no parsed metadata

#### Scenario: Exactly 1 MiB
- **WHEN** a client delivers a complete body of exactly 1,048,576 bytes
- **THEN** the inbox holds the whole body with `truncated = false`

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
