## MODIFIED Requirements

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

### Requirement: Oversized bodies
After reading a complete body larger than 1 MiB (1,048,576 bytes), intake SHALL commit an inbox entry with the size flag set and zero body bytes, then respond `202`. The entry SHALL have no parsed metadata. A complete body of exactly 1 MiB SHALL be stored unchanged with the size flag clear.

#### Scenario: 2 MiB body
- **WHEN** a client delivers a complete 2 MiB body, even if the prefix contains well-formed metadata
- **THEN** the system responds `202` after committing an inbox entry with `truncated = true`, an empty body and no parsed metadata

#### Scenario: Exactly 1 MiB
- **WHEN** a client delivers a complete body of exactly 1,048,576 bytes
- **THEN** the inbox holds the whole body with `truncated = false`
