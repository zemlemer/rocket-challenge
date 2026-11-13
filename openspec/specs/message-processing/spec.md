# message-processing Specification

## Purpose
Works through the inbox: claims unprocessed entries, applies or quarantines them, and marks them as processed, with an exactly-once effect per entry, safety under concurrent processors and crashes, and enough throughput to keep up with the senders.

## Requirements

### Requirement: Claim, apply and mark
The processor SHALL claim unprocessed inbox entries, validate each one (see message-validation), apply conforming ones to their rocket's state (see rocket-state), and mark every claimed entry as processed with its outcome — `applied`, `quarantined` or `failed` — in the same transaction as its effect. Processed entries SHALL stay in the inbox.

#### Scenario: Entry applied
- **WHEN** a conforming `RocketSpeedIncreased` entry is unprocessed and a processor runs
- **THEN** the rocket's speed includes its delta and the entry is marked processed with outcome `applied` and a processing time

### Requirement: Exactly-once effect
Every inbox entry SHALL be processed exactly once: an entry marked as processed SHALL never be claimed again, and an entry SHALL never be applied without being marked in the same transaction. Together with intake's one entry per identity, every message identity has its effect at most once.

#### Scenario: Concurrent processors
- **WHEN** four processors run while entries arrive
- **THEN** every entry is processed by exactly one of them and applied once

### Requirement: Concurrent processors
Several processors SHALL be able to run at the same time and claim disjoint sets of entries. The result SHALL equal that of a single processor processing the same entries, whatever rockets the entries belong to and in whatever order they are claimed.

#### Scenario: Four processors, shuffled stream
- **WHEN** the shuffled messages of several rockets are in the inbox and four processors run concurrently
- **THEN** every rocket's state equals the state after processing its messages once in order

### Requirement: Failed entries
If applying an entry fails for a reason other than the database being unavailable, the processor SHALL isolate it: the other entries of its batch SHALL still be applied, and the failing entry SHALL be marked with outcome `failed`, a reason containing the error, and logged as an error. `failed` means a valid message the system could not apply, unlike `quarantined`, which means an invalid message.

#### Scenario: One entry breaks a batch
- **WHEN** a batch of 100 entries contains one whose state update is rejected by the database
- **THEN** the other 99 are applied and marked `applied`, and that one is marked `failed` with the database error as reason

#### Scenario: Database unavailable is not a failure
- **WHEN** the database becomes unreachable while a batch is processed
- **THEN** no entry is marked `failed`; the batch is retried after the database is back

### Requirement: Crash safety
If a processor stops or crashes while processing, every claimed entry SHALL either be applied and marked, or neither applied nor marked and available to the next claim.

#### Scenario: Failure before commit
- **WHEN** processing of a batch fails before it is committed and processing runs again
- **THEN** every entry of the batch is applied exactly once

### Requirement: Stuck processors do not block others
A processor that stops making progress in the middle of a batch — hung, or cut off from the database without the connection being closed — SHALL lose its claim and its locks within 30 seconds, without effect, so that the other processors process those entries and update those rockets.

#### Scenario: Processor hangs mid-batch
- **WHEN** a processor has claimed a batch and stops responding before committing
- **THEN** within 30 seconds its entries are claimed by another processor and applied once, and its own batch has no effect

#### Scenario: Waiting behind a stuck rocket update
- **WHEN** a processor waits more than 5 seconds for a rocket row locked by another processor
- **THEN** it rolls back its batch without marking anything and retries after a backoff, like on an unavailable database
After the database becomes reachable again, processing SHALL resume without restarting the processor.

#### Scenario: Database restarted
- **WHEN** the database is unreachable for a while and then reachable again
- **THEN** entries stored before and after the outage are processed without manual intervention

#### Scenario: Backoff while unavailable
- **WHEN** the database stays unreachable
- **THEN** each processor retries after growing delays — 100 ms, doubling, at most 2 s — and returns to normal pace after the first successful batch

### Requirement: Throughput and freshness
Under the provided simulator's default load (about 4,000 messages per second over 20 rockets) the processors SHALL keep up: the oldest unprocessed entry SHALL never be older than 1 second, and no entry SHALL be unprocessed 2 seconds after the simulator finishes.

#### Scenario: Default simulator run
- **WHEN** the simulator runs with its default options against the stack
- **THEN** the oldest unprocessed entry is never older than 1 second and none is left 2 seconds after the run
