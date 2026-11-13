# Proposal

## Why

Intake now stores every message in the inbox, but nothing turns those messages into rocket state yet. This iteration adds a processor that works through the inbox and maintains each rocket's current state — correctly despite out-of-order delivery and duplicates, with an exactly-once effect per message, at the simulator's default load of about 4,000 messages per second.

## What Changes

- New mode `rockets processor`, deployed as four instances next to the servers. It claims unprocessed inbox entries, applies them to rocket state and **marks them as processed** in the same transaction — entries are never deleted, so the inbox is the full record of what arrived and what was done with it.
- **Exactly-once effect**: intake's unique index already keeps one inbox entry per message identity, also after it has been processed; the processor claims each unprocessed entry once (`FOR UPDATE SKIP LOCKED`) and marks it in the transaction that applies it.
- **Order-independent rocket state, written as atomic updates**:
  - speed is changed by increments (`speed = speed + delta`), never by read-modify-write, and never clamped;
  - mission is written only if the message's number is higher than `mission_message_number`, the number of the message the current mission came from, so an older message never overwrites a newer one;
  - the launch is always message number 1 — a `RocketLaunched` with another number, or a message 1 of another type, is quarantined — so launch fields are written once and the launch speed is simply added;
  - the explosion is written once and is terminal; any further `RocketExploded` of the same rocket is quarantined, whatever its order;
  - the number and time of the last applied message show how current a rocket's state is.
- **Validation and quarantine**: entries that do not conform — metadata missing, unknown type, payload not matching its type, numbers out of range — are marked `quarantined` with a reason in the inbox and do not change state. Valid entries the system cannot apply (a bug) are isolated and marked `failed`, so they never stop processing.
- **Throughput and freshness**: the processors keep up with the simulator's default load and keep the oldest unprocessed entry younger than one second.
- Migration `0002_processing`: processing columns and a pending-entries index on `inbox`, and the `rockets` state table.

Not in this iteration: query endpoints (`GET /rockets…`) — state is verified through the database and tests; retention or archiving of processed inbox entries.

## Capabilities

### New Capabilities

- `message-processing`: claiming and marking inbox entries, exactly-once effect, concurrent processors, crash safety, outage recovery, throughput and freshness.
- `message-validation`: what makes an inbox entry a conforming message, and quarantine of the others.
- `rocket-state`: how a rocket's processed messages determine its state, independently of arrival order.
- `processor-runtime`: the `processor` mode — fixed settings, schema version check, graceful shutdown.

### Modified Capabilities

- `database-schema`: adds processing status to inbox entries and the rocket state storage.
- `deployment`: adds four processor instances to the stack.

## Impact

- New packages: `internal/domain` (validation and combine rules, pure Go), `internal/processor` (processing loop; owns its repository interface); `internal/repository` gains the processing repository; `cmd/rockets` gains the `processor` subcommand.
- Migration `0002_processing.sql`; `compose.yaml` gains the `processor` service.
- No change to the `POST /messages` contract. Because processed entries stay in the inbox, a redelivery of an already processed message is now dropped at intake instead of reaching the processor.
