# Spec Delta

## Purpose

Defines how the messages processed for a rocket determine its state, so that the state is the same whatever order the messages were processed in.

## ADDED Requirements

### Requirement: Order-independent state
Every conforming message SHALL be applied when it is processed, without waiting for missing earlier messages. For any set of messages of one rocket, the resulting state SHALL be the same regardless of processing order and of how the messages are split into batches and between processors.

#### Scenario: Shuffled processing
- **WHEN** messages 1..10 of a rocket are processed in a random order, in random batches
- **THEN** the rocket's state equals the state after processing 1..10 once in order

#### Scenario: Message after a gap
- **WHEN** messages 1, 2 and 5 of a rocket are processed and 3 and 4 have not arrived
- **THEN** the effects of messages 1, 2 and 5 are already in the rocket's state

### Requirement: Speed by increments
A rocket's speed SHALL be changed only by adding increments: the launch speed of its launch message, plus each `RocketSpeedIncreased.by`, minus each `RocketSpeedDecreased.by`. A stored speed SHALL never be overwritten with a value computed from a previously read one. Speed SHALL NOT be clamped; a negative value is kept as is.

#### Scenario: Deltas before launch
- **WHEN** `RocketSpeedIncreased` (`by` 300) number 2 is processed before `RocketLaunched` (`launchSpeed` 500) number 1
- **THEN** after both the speed is 800

#### Scenario: Negative speed
- **WHEN** a rocket launched at 100 processes `RocketSpeedDecreased` with `by` 300
- **THEN** its speed is -200

### Requirement: Mission by newest message
A rocket's state SHALL keep, next to its mission, `mission_message_number`: the number of the message the mission came from. A `RocketLaunched` or `RocketMissionChanged` message SHALL set the mission only if its number is higher than `mission_message_number`.

#### Scenario: Older mission change arrives late
- **WHEN** `RocketMissionChanged` number 7 (`MARS`) is processed and then number 4 (`MOON`)
- **THEN** the mission remains `MARS` with `mission_message_number` 7

### Requirement: Launch attributes
A rocket's type, launch speed and launch time SHALL come from its launch message, which is always message number 1 (see message-validation). Until it is processed the rocket SHALL be marked as not launched, with no launch speed, while speed increments already add up.

#### Scenario: Not launched yet
- **WHEN** only speed changes of a rocket have been processed
- **THEN** the rocket is marked as not launched, its launch speed is empty and its speed holds the accumulated increments

#### Scenario: Launch arrives after speed changes
- **WHEN** `RocketSpeedIncreased` (`by` 300) number 2 is processed and then `RocketLaunched` (`launchSpeed` 500) number 1
- **THEN** the rocket is launched with launch speed 500 and speed 800

### Requirement: Explosion once
The first `RocketExploded` message processed for a rocket SHALL mark it as exploded with its reason; the exploded status is terminal. Any further `RocketExploded` message for that rocket SHALL be quarantined, whatever its number and order of arrival. Other messages numbered after the explosion SHALL still be applied.

#### Scenario: Second explosion
- **WHEN** a rocket has exploded with reason `FROZEN_O_RINGS` and another `RocketExploded` arrives with reason `SYSTEM_MALFUNCTION`
- **THEN** the second one is quarantined with a reason saying the rocket already exploded, and the reason stays `FROZEN_O_RINGS`

#### Scenario: Explosion before an earlier message
- **WHEN** `RocketExploded` number 9 is processed and then `RocketSpeedIncreased` number 8
- **THEN** the rocket stays exploded and its speed includes the increase from number 8

### Requirement: Last applied message
A rocket's state SHALL record `last_applied_message_number` and `last_applied_message_at`: the number and message time of the highest-numbered message applied to it. Quarantined and failed messages SHALL NOT change them.

#### Scenario: Older message arrives late
- **WHEN** message 5 is applied and then message 4
- **THEN** `last_applied_message_number` stays 5 with the message time of message 5

#### Scenario: Quarantined message ignored
- **WHEN** messages 1 and 2 are applied and message 3 is quarantined
- **THEN** `last_applied_message_number` is 2
