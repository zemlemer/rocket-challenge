## MODIFIED Requirements

### Requirement: Conforming message
A conforming entry SHALL have `truncated = false`, an identity with a valid channel, a message time and a known type. Its body SHALL be a JSON object with a `message` object satisfying that type's payload rules. Metadata SHALL come from inbox columns. Extra body fields SHALL be ignored. Known types are `RocketLaunched`, `RocketSpeedIncreased`, `RocketSpeedDecreased`, `RocketExploded` and `RocketMissionChanged`. A set `truncated` flag SHALL cause rejection for exceeding the size limit.

#### Scenario: Simulator message
- **WHEN** the entry holds `{"metadata":{"channel":"c1","messageNumber":1,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`
- **THEN** it is conforming

#### Scenario: Missing message time
- **WHEN** an entry has an identity and a known type but no message time
- **THEN** it is non-conforming

#### Scenario: Message is not an object
- **WHEN** an entry's body has no `message`, or `message` is `null`, an array, a string or a number
- **THEN** intake has stored it unchanged, and processing quarantines it with a reason saying `message` must be an object

#### Scenario: Oversized request record
- **WHEN** the processor claims an entry with `truncated = true` and zero body bytes
- **THEN** it marks the entry `quarantined` with a reason indicating the body exceeded the size limit and changes no rocket state

#### Scenario: Historical truncated entry
- **WHEN** the processor claims a historical entry with `truncated = true` and a retained body prefix
- **THEN** it quarantines the entry, keeps its prefix and changes no rocket state
