# Spec Delta

## Purpose

Defines when an inbox entry is a conforming rocket message that may change rocket state, and how the others are quarantined in place for investigation.

## ADDED Requirements

### Requirement: Conforming message
An inbox entry SHALL be conforming when it is not truncated, has an identity, a message time and one of the message types `RocketLaunched`, `RocketSpeedIncreased`, `RocketSpeedDecreased`, `RocketExploded`, `RocketMissionChanged`, and its body is a JSON object whose `message` object satisfies the payload rules of that type. Metadata is taken from the inbox columns written by intake; unknown extra fields in the body SHALL be ignored.

#### Scenario: Simulator message
- **WHEN** the entry holds `{"metadata":{"channel":"c1","messageNumber":1,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`
- **THEN** it is conforming

#### Scenario: Missing message time
- **WHEN** an entry has an identity and a known type but no message time
- **THEN** it is non-conforming

#### Scenario: Message is not an object
- **WHEN** an entry's body has no `message`, or `message` is `null`, an array, a string or a number
- **THEN** intake has stored it unchanged, and processing quarantines it with a reason saying `message` must be an object

### Requirement: Launch is message number 1
A `RocketLaunched` message SHALL be conforming only with message number 1, and a message with number 1 SHALL be conforming only if it is a `RocketLaunched` message. A rocket therefore has at most one launch, and which one does not depend on arrival order.

#### Scenario: Launch with another number
- **WHEN** an entry is a `RocketLaunched` message with message number 6
- **THEN** it is quarantined with a reason saying a launch must be message number 1

#### Scenario: First message is not a launch
- **WHEN** an entry has message number 1 and type `RocketSpeedIncreased`
- **THEN** it is quarantined with a reason saying message number 1 must be a launch

### Requirement: Payload rules
Payloads SHALL satisfy, per message type: `RocketLaunched` — `type` and `mission` non-empty strings, `launchSpeed` an integer from 0 to 1,000,000,000; `RocketSpeedIncreased` and `RocketSpeedDecreased` — `by` an integer from 0 to 1,000,000,000; `RocketExploded` — `reason` a non-empty string; `RocketMissionChanged` — `newMission` a non-empty string.

#### Scenario: Negative delta
- **WHEN** a `RocketSpeedDecreased` payload has `by` `-5`
- **THEN** the entry is non-conforming

#### Scenario: Out of range
- **WHEN** a `RocketSpeedIncreased` payload has `by` `1000000001`
- **THEN** the entry is non-conforming

#### Scenario: Fractional speed
- **WHEN** a `RocketLaunched` payload has `launchSpeed` `500.5`
- **THEN** the entry is non-conforming

#### Scenario: Unknown type
- **WHEN** an entry has message type `RocketTeleported`
- **THEN** the entry is non-conforming

### Requirement: Payload field format
Every required payload field SHALL be present and not `null` — a missing `by` is an error, never 0. Every string SHALL be at most 255 bytes and contain no NUL; every integer SHALL be a JSON integer literal.

#### Scenario: Missing required field
- **WHEN** a `RocketSpeedIncreased` payload is `{}` or has `"by": null`
- **THEN** the entry is non-conforming and is not applied as an increment of 0

#### Scenario: Overlong string
- **WHEN** a `RocketMissionChanged` payload has a 256-byte `newMission`
- **THEN** the entry is non-conforming

### Requirement: Quarantine in place
A non-conforming entry SHALL be marked as processed with outcome `quarantined` and a reason naming the first rule it broke, logged as a warning, and SHALL NOT change any rocket state. It stays in the inbox with its body and metadata for investigation.

#### Scenario: Quarantined with reason
- **WHEN** an entry with a negative `by` is processed
- **THEN** it is marked `quarantined` with a reason naming `by`, and no rocket state changed

#### Scenario: Entry without identity
- **WHEN** an entry without identity (for example invalid JSON) is processed
- **THEN** it is marked `quarantined`

