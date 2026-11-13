# Spec Delta

## Purpose

Lets clients such as a dashboard read the current processed state of launched or exploded rockets over HTTP, one rocket at a time or as a sorted list.

## ADDED Requirements

### Requirement: Single rocket
`GET /rockets/{channel}` SHALL respond `200` with the rocket representation of the visible rocket whose channel equals the percent-decoded path segment exactly (case-sensitive), and `404` with `{"error": "rocket not found"}` when there is none. Query parameters SHALL be ignored. Responses SHALL have `Content-Type: application/json`.

#### Scenario: Launched rocket
- **WHEN** the launch of channel `c1` (`Falcon-9`, `launchSpeed` 500, mission `ARTEMIS`) and `RocketSpeedIncreased` (`by` 300) number 2 have been processed, and a client sends `GET /rockets/c1`
- **THEN** the server responds `200` with `channel` `c1`, `type` `Falcon-9`, `status` `active`, `speed` 800, `launchSpeed` 500, `mission` `ARTEMIS`

#### Scenario: Unknown channel
- **WHEN** no message of channel `c9` has been processed and a client sends `GET /rockets/c9`
- **THEN** the server responds `404` with `{"error": "rocket not found"}`

#### Scenario: Different case
- **WHEN** rocket `abc-1` is visible and a client sends `GET /rockets/ABC-1`
- **THEN** the server responds `404`

### Requirement: Read methods
`/rockets` and `/rockets/{channel}` SHALL accept `GET` and `HEAD` (`HEAD` answers like `GET` without a body). Other methods SHALL get `405` with `Allow: GET, HEAD`, following the server's rule for unsupported methods.

#### Scenario: HEAD request
- **WHEN** a client sends `HEAD /rockets`
- **THEN** the server responds `200` without a body

#### Scenario: Write method
- **WHEN** a client sends `POST /rockets` or `DELETE /rockets/c1`
- **THEN** the server responds `405` with `Allow: GET, HEAD` and an `error` field

### Requirement: Canonical paths
A request path containing `.` or `..` segments or empty segments SHALL be redirected (`307`) to its cleaned form before any rocket lookup; a path with more segments than `/rockets/{channel}` or an empty channel SHALL get `404`.

#### Scenario: Dot segments
- **WHEN** a client sends `GET /rockets/./c1` or `GET /rockets//c1`
- **THEN** the server responds `307` with `Location: /rockets/c1`

#### Scenario: Extra segment
- **WHEN** a client sends `GET /rockets/c1/x` or `GET /rockets/`
- **THEN** the server responds `404`

### Requirement: Rocket list
`GET /rockets` SHALL respond `200` with `{"rockets": [...]}` holding the representation of every visible rocket exactly once, without pagination. With no visible rocket it SHALL respond `{"rockets": []}`.

#### Scenario: Several rockets
- **WHEN** the launches of channels `a`, `b` and `c` have been processed and a client sends `GET /rockets`
- **THEN** the response holds exactly the rockets `a`, `b` and `c`

#### Scenario: Empty system
- **WHEN** no launch and no explosion has been processed and a client sends `GET /rockets`
- **THEN** the server responds `200` with `{"rockets": []}`

### Requirement: Visibility of launched and exploded rockets
A rocket SHALL be visible once its launch message or a `RocketExploded` message has been processed. Stored state of a rocket that has neither SHALL NOT appear in any response. A visible rocket SHALL stay visible.

#### Scenario: Speed change before launch
- **WHEN** only `RocketSpeedIncreased` number 2 of channel `c1` has been processed
- **THEN** `GET /rockets/c1` responds `404` and `GET /rockets` does not contain `c1`

#### Scenario: Launch arrives later
- **WHEN** the launch of `c1` is then processed
- **THEN** `GET /rockets/c1` responds `200` with a speed that includes the earlier increase

#### Scenario: Exploded rocket
- **WHEN** a launched rocket has processed `RocketExploded` with reason `FROZEN_O_RINGS`
- **THEN** it is still returned, with `status` `exploded` and `explosionReason` `FROZEN_O_RINGS`

#### Scenario: Explosion before launch
- **WHEN** `RocketSpeedIncreased` (`by` 300) number 2 and `RocketExploded` number 5 of channel `c1` are processed before its launch
- **THEN** `c1` is returned with `status` `exploded`, `speed` 300, and `type`, `launchSpeed`, `launchedAt` and `mission` `null`

#### Scenario: Launch after explosion
- **WHEN** the launch of that `c1` (`Falcon-9`, `launchSpeed` 500, mission `ARTEMIS`) is then processed
- **THEN** `c1` is returned with `status` `exploded`, `type` `Falcon-9`, `speed` 800 and the launch attributes filled in

### Requirement: Hidden invalid channels
A stored rocket whose channel does not satisfy the channel rule of message validation (only ASCII letters, digits, `-` and `_`, 1 to 255 bytes) SHALL NOT be visible, even when it is launched or exploded. A request for a channel that does not satisfy the rule SHALL get `404` like an unknown channel. Stored rocket rows SHALL NOT be deleted or changed by this rule.

#### Scenario: Historical row with a dot
- **WHEN** the database holds a launched rocket row with channel `a.b`, stored before the channel rule existed
- **THEN** `GET /rockets` does not contain it, `GET /rockets/a.b` responds `404`, and the row is still in the database

#### Scenario: Percent-encoded characters outside the rule
- **WHEN** a client sends `GET /rockets/a%2Fb` or `GET /rockets/%E2%82%AC`
- **THEN** the decoded channel (`a/b`, `€`) breaks the rule and the server responds `404`

#### Scenario: Same rule as processing
- **WHEN** a channel example is accepted or rejected by message validation
- **THEN** a launched rocket row with that channel is visible or hidden accordingly

### Requirement: Rocket representation
Both endpoints SHALL represent a rocket with the same JSON fields: `channel`, `type`, `status`, `speed`, `launchSpeed`, `mission`, `launchedAt`, `explosionReason`, `lastMessageNumber` and `lastMessageAt`. `status` SHALL be `exploded` when the rocket has exploded and `active` otherwise; `explosionReason` SHALL be `null` while active. Every field SHALL be present even when `null`. Speeds and numbers SHALL be JSON integers.

#### Scenario: Same representation
- **WHEN** a client reads rocket `c1` through `GET /rockets/c1` and through `GET /rockets`
- **THEN** both representations of `c1` are equal

#### Scenario: Negative speed
- **WHEN** a rocket launched at 100 has processed `RocketSpeedDecreased` with `by` 300
- **THEN** its `speed` is `-200`

### Requirement: Fields before launch
For an exploded rocket whose launch is not processed yet, `type`, `launchSpeed` and `launchedAt` SHALL be `null`, `mission` SHALL be `null` unless a mission change was processed, and `speed` SHALL hold the applied increments without a launch speed. Once the launch is processed, these fields SHALL reflect it.

#### Scenario: Mission known before launch
- **WHEN** `RocketMissionChanged` (`MARS`) number 3 and `RocketExploded` number 4 of a rocket are processed before its launch
- **THEN** the rocket is returned with `mission` `MARS` and `type` `null`

### Requirement: Time fields
`launchedAt` and `lastMessageAt` SHALL be the message times sent by the rocket, not processing times, and SHALL NOT be checked against the clock or each other. They SHALL be written in RFC 3339 in UTC with microsecond precision and no trailing zeros in the fraction. A time whose UTC year is outside 0000–9999 SHALL be written with all digits of its year and a leading `-` when negative, and SHALL NOT make a response fail.

#### Scenario: Message time in UTC
- **WHEN** the launch of `c1` had message time `2022-02-02T19:39:05.86337+01:00`
- **THEN** `launchedAt` is `2022-02-02T18:39:05.86337Z`

#### Scenario: Nanoseconds sent
- **WHEN** the launch had message time `2022-02-02T19:39:05.123456789+01:00`
- **THEN** `launchedAt` is `2022-02-02T18:39:05.123457Z`

#### Scenario: Year beyond 9999 in UTC
- **WHEN** a visible rocket's launch had message time `9999-12-31T23:30:00-01:00`
- **THEN** `GET /rockets` responds `200` and that rocket's `launchedAt` is `10000-01-01T00:30:00Z`

#### Scenario: Times from the sender's clock
- **WHEN** a rocket's highest-numbered message has a message time earlier than its launch
- **THEN** both times are returned as sent

### Requirement: Last message fields
`lastMessageNumber` and `lastMessageAt` SHALL be the number and message time of the highest-numbered message applied to the rocket. They SHALL NOT imply that every lower-numbered message has been applied.

#### Scenario: Gap in applied messages
- **WHEN** messages 1, 2 and 5 of a rocket have been applied and 3 and 4 have not arrived
- **THEN** `lastMessageNumber` is 5

### Requirement: Sorting
`GET /rockets` SHALL order rockets by the field named in `sort` — `channel`, `type`, `mission`, `speed`, `status` or `launchedAt`, default `channel` — in the direction named in `order` — `asc` or `desc`, default `asc`. Text SHALL compare byte-wise; `status` SHALL order `active` before `exploded` ascending. Rockets with `null` in the sort field SHALL come last in either direction. Rockets equal in the sort field, including all `null`, SHALL be ordered by ascending `channel` in either direction.

#### Scenario: Fastest first
- **WHEN** rockets `a` (speed 100), `b` (speed 300) and `c` (speed 200) are visible and a client sends `GET /rockets?sort=speed&order=desc`
- **THEN** the rockets are returned in the order `b`, `c`, `a`

#### Scenario: Default order
- **WHEN** rockets `b`, `a` and `C` are visible and a client sends `GET /rockets`
- **THEN** the rockets are returned in the order `C`, `a`, `b`

#### Scenario: Ties broken by channel
- **WHEN** rockets `b` and `a` have the same mission and a client sends `GET /rockets?sort=mission&order=desc`
- **THEN** `a` is returned before `b`

#### Scenario: Unlaunched rockets last
- **WHEN** rockets `a` (`type` `Soyuz`) and `c` (`type` `Falcon-9`) are launched, exploded rockets `b` and `d` are not, and a client sends `GET /rockets?sort=type&order=desc`
- **THEN** the rockets are returned in the order `a`, `c`, `b`, `d`

#### Scenario: Status order
- **WHEN** rocket `a` has exploded, rocket `b` is active, and a client sends `GET /rockets?sort=status`
- **THEN** `b` is returned before `a`

### Requirement: Invalid query parameters
`GET /rockets` SHALL respond `400` with an `error` naming the parameter and its accepted values when `sort` or `order` has an unknown value (values are case-sensitive), is empty, or is given more than once, and `400` when the query string is not validly percent-encoded. Other query parameters SHALL be ignored.

#### Scenario: Unknown sort field
- **WHEN** a client sends `GET /rockets?sort=altitude`
- **THEN** the server responds `400` with an `error` naming `sort` and the accepted fields

#### Scenario: Repeated direction
- **WHEN** a client sends `GET /rockets?order=asc&order=desc`
- **THEN** the server responds `400` with an `error` naming `order`

#### Scenario: Wrong case
- **WHEN** a client sends `GET /rockets?sort=Speed` or `GET /rockets?order=DESC`
- **THEN** the server responds `400`

#### Scenario: Malformed query string
- **WHEN** a client sends `GET /rockets?sort=%zz&order=desc`
- **THEN** the server responds `400` instead of silently dropping `sort`

#### Scenario: Unrelated parameter
- **WHEN** a client sends `GET /rockets?page=2`
- **THEN** the server responds `200` with the full list

### Requirement: Read availability
Both endpoints SHALL respond `503` with an `error` field when the database is unavailable or does not answer within 5 s, and `500` on other failures. A request abandoned by its client SHALL NOT be logged as a server error. The endpoints SHALL keep answering from stored state while no processor is running.

#### Scenario: Database down
- **WHEN** the database is unreachable and a client sends `GET /rockets`
- **THEN** the server responds `503` with an `error` field

#### Scenario: Slow database
- **WHEN** the read query does not complete within 5 s
- **THEN** the server responds `503`

#### Scenario: Client disconnects
- **WHEN** a client closes the connection while its `GET /rockets` is being served
- **THEN** the query is cancelled and no error-level log entry is written

#### Scenario: Processors stopped
- **WHEN** all processors are stopped and a client sends `GET /rockets/c1` for a launched rocket
- **THEN** the server responds `200` with its last processed state

### Requirement: Read consistency
A response SHALL reflect committed processing results only, without waiting for inbox entries that are not processed yet. A `202` from `POST /messages` SHALL NOT guarantee that a following read includes that message. Each list response SHALL come from one consistent snapshot, and reads SHALL NOT block processing.

#### Scenario: Read before processing
- **WHEN** a message has been answered `202` but not processed yet, and a client reads its rocket
- **THEN** the response shows the state without that message, and a later read after processing includes it

#### Scenario: Read during processing
- **WHEN** a client lists rockets while a processor is applying a batch
- **THEN** every rocket in the response shows its state either before or after that batch, and the batch completes without waiting for the read
