# Spec Delta

## MODIFIED Requirements

### Requirement: Conforming message
An inbox entry SHALL be conforming when it is not truncated, has an identity whose channel contains only ASCII letters, digits, `-`, and `_`, has a message time and one of the message types `RocketLaunched`, `RocketSpeedIncreased`, `RocketSpeedDecreased`, `RocketExploded`, `RocketMissionChanged`, and its body is a JSON object whose `message` object satisfies the payload rules of that type. Metadata is taken from the inbox columns written by intake; unknown extra fields in the body SHALL be ignored.

#### Scenario: Simulator message
- **WHEN** the entry holds `{"metadata":{"channel":"c1","messageNumber":1,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`
- **THEN** it is conforming

#### Scenario: Missing message time
- **WHEN** an entry has an identity and a known type but no message time
- **THEN** it is non-conforming

#### Scenario: Message is not an object
- **WHEN** an entry's body has no `message`, or `message` is `null`, an array, a string or a number
- **THEN** intake has stored it unchanged, and processing quarantines it with a reason saying `message` must be an object

#### Scenario: URL-safe channel
- **WHEN** an otherwise valid message has channel `Rocket_7-Az09`
- **THEN** it is conforming

#### Scenario: URL-significant channel
- **WHEN** an otherwise valid message has a channel containing `/`, `.`, `%`, `?`, `#`, a space, or a non-ASCII character
- **THEN** intake stores and acknowledges it, but processing quarantines it with a reason naming the channel and changes no rocket state
