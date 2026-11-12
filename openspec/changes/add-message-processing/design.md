# Design

## Context

Builds on the archived changes `add-migrate-mode` and `add-message-intake`. Intake stores every `POST /messages` body in `inbox` with its parsed metadata (`channel`, `message_number`, `message_time`, `message_type`, `truncated`); a partial unique index keeps one entry per identity. Measured load (`docs/simulator-notes.md`): 20 rockets, about 4,000 messages per second, numbers contiguous from 1, shallow reordering (≤ 2 positions). Specs: `message-processing`, `message-validation`, `rocket-state`, `processor-runtime`, a modification of `message-intake`, additions to `database-schema` and `deployment`.

## Goals / Non-Goals

**Goals:**
- Rocket state as a deterministic function of the *set* of processed messages, whatever the order, batching and processor.
- Exactly-once effect per entry, across processor instances, crashes and restarts.
- Inbox as the complete record: every entry stays, with when and how it was processed.
- Keep up with about 4,000 messages per second with the oldest unprocessed entry younger than 1 s.
- Business rules testable without a database.

**Non-Goals:**
- Query endpoints and the API representation of a rocket (next iteration).
- Retention, archiving or partitioning of processed inbox entries.
- Replaying quarantined entries automatically.

## Decisions

### D1. Processed entries are marked, not deleted

`0002_processing.sql` adds to `inbox`: `processed_at timestamptz null`, `outcome text null` (`applied` | `quarantined` | `failed`), `reason text null`, and a partial index `inbox_pending_idx on inbox (id) where processed_at is null`.

Consequences:
- **The inbox answers the sender.** Entries are never deleted, so the unique index on (`channel`, `message_number`) covers processed entries too: a redelivery is never stored again and gets the same `202`, whether the first copy is waiting, applied or quarantined. Responses depend only on intake and the inbox, never on processing, and no duplicate reaches the processor — no processor-side duplicate tracking is needed.
- **Quarantine is a status**, not a table: the entry stays where it was, with `outcome = 'quarantined'` and `reason`.
- **History for free:** the inbox shows what arrived, when, and what happened to it.
- **Cost:** the inbox grows with the stream (≈ 30 MB per 100,000 messages). Retention is a non-goal here; the pending index keeps the processor's work independent of the processed history.

*Alternative considered:* deleting processed entries and tracking duplicates per rocket with a watermark and a set of numbers above it — bounded storage, but duplicate detection had to be rebuilt in the processor and history was lost.

### D2. Claiming with `FOR UPDATE SKIP LOCKED`

One transaction per batch:

```sql
SELECT id, received_at, channel, message_number, message_time, message_type, body, truncated
FROM inbox
WHERE processed_at IS NULL
ORDER BY id
LIMIT $batch
FOR UPDATE SKIP LOCKED
```

Rows locked by one processor are skipped by the others, so processors take disjoint batches without coordination, oldest first. Marking — `UPDATE inbox SET processed_at = now(), outcome = …, reason = … WHERE id = ANY($ids)` — happens in the same transaction as the state updates: commit applies and marks together, rollback does neither (exactly-once, crash safety).

**Session limits.** A transaction holding claimed rows and rocket row locks must not outlive its processor. Postgres frees them only when the session ends, and a hung process or a connection cut off without a reset (network partition) keeps the session alive until TCP keepalive notices — about 2 hours by default. The processor's pool therefore sets, per connection (`RuntimeParams` and pgx keepalive options; constants with comments, not flags — they are "something is wrong" thresholds, not tuning):

| Setting | Value | Effect |
|---|---|---|
| `idle_in_transaction_session_timeout` | 30 s | Postgres ends a session idle inside a transaction; its locks are released and its batch rolled back |
| `statement_timeout` | 30 s | no processor statement runs longer (a normal batch takes milliseconds) |
| `lock_timeout` | 5 s | a statement waiting for a rocket row longer fails; treated as unavailable (D5a): roll back, mark nothing, back off |
| TCP keepalive | idle 30 s, interval 10 s, 3 probes | a dead connection is detected in about a minute instead of hours |

The server does not need these: its only transaction is a single `INSERT`.

*Alternative considered:* an explicit `in_progress` status with a lease and a sweeper that returns expired claims. It is needed when claiming and processing are separate transactions (e.g. work outside the database); here they are one transaction, the row lock *is* the in-progress state, and Postgres releases it itself. A status would double the writes per entry and require fencing so that a slow-but-alive processor and the sweeper never both apply an entry.

*Alternatives considered:* per-rocket advisory locks and batches per rocket (correct, but needs a channel-listing query and ties each rocket to one processor at a time); a single processor (no availability).

### D3. State written as atomic, order-independent updates

Every rule of rocket-state is a sum, a "higher number wins" rule or a "written once" rule, so it can be applied by whichever processor claims the entry, without reading the state in the application first:

| Column(s) | Update |
|---|---|
| `speed` | `speed + delta` |
| `mission`, `mission_message_number` | set only if the new number > `mission_message_number` |
| `rocket_type`, `launch_speed`, `launched_at` | written once, by message number 1 — the only conforming launch (validation); `speed + launch_speed` |
| `explosion_reason` | written once, only while empty; a later `RocketExploded` finds it set and is quarantined |
| `last_applied_message_number`, `last_applied_message_at` | max by number over applied entries |

`mission_message_number` is "the number of the message the current mission came from"; comparing against it is what prevents an older message from overwriting a newer mission.

A rocket has at most one launch: validation accepts `RocketLaunched` only as message number 1 and message number 1 only as `RocketLaunched`, and intake stores one entry per identity. So launch fields are written once and the launch speed is simply added to `speed` — no replacement, no condition, no race between processors.

The explosion is written once as well, but its number is not known in advance, so "once" is decided at write time: `UPDATE rockets SET explosion_reason = $reason WHERE channel = $channel AND explosion_reason IS NULL`. If it updates no row, the rocket had already exploded and the entry is marked `quarantined` (`rocket already exploded`) in the same transaction. Within one batch, `Fold` keeps the first explosion by inbox id and quarantines the rest. Which of two explosions is kept therefore depends on arrival order — acceptable, because a second explosion breaks the protocol ("sent out once") and is never applied.

### D4. Folding a batch per rocket

`internal/domain` folds the conforming messages of a batch into one `Delta` per rocket: summed speed increments, the launch (if message 1 is in the batch), the highest mission candidate, the first explosion, the highest applied number with its time. `Delta.Merge` is associative and commutative — the property the tests check. The repository applies each `Delta` with one upsert whose `ON CONFLICT DO UPDATE` expressions implement the table in D3 (same merge, in SQL):

Parameters keep the two parts of speed apart: `$delta_sum` (increments only) and the launch fields (`NULL` when message 1 is not in the batch).

```sql
INSERT INTO rockets (channel, speed, launch_speed, rocket_type, launched_at, mission, mission_message_number, …)
VALUES ($channel, $delta_sum + coalesce($launch_speed, 0), $launch_speed, $type, $launched_at, $mission, $mission_number, …)
ON CONFLICT (channel) DO UPDATE SET
  speed        = rockets.speed + $delta_sum + coalesce($launch_speed, 0),
  launch_speed = coalesce($launch_speed, rockets.launch_speed),     -- written once: only message 1 carries it
  rocket_type  = coalesce($type, rockets.rocket_type),
  launched_at  = coalesce($launched_at, rockets.launched_at),
  mission      = CASE WHEN $mission_number > rockets.mission_message_number THEN $mission ELSE rockets.mission END,
  mission_message_number = greatest(rockets.mission_message_number, $mission_number),
  …
```

Upserts within a batch run in channel order, so processors updating overlapping rockets always lock rows in the same order and cannot deadlock; two updates of the same rocket serialise on its row for one statement.

### D5. Validation in the domain

`domain.Classify(entry)` returns a typed `Message` or a `Rejection{Reason}`: metadata from the columns (identity, time, known type, not truncated), then `message` must be a JSON object, and only it is decoded for the entry's type — into `json.RawMessage` fields first, so a missing or `null` required field is an error and never a zero value (`json.Decoder` with `UseNumber`; integers checked as JSON integer literals like intake's `messageNumber`, with `by` and `launchSpeed` limited to 0…1,000,000,000; strings ≤ 255 bytes, no NUL, because they are stored in `text`). The reason names the first rule broken (e.g. `message.by: must be an integer ≥ 0`). Validation mirrors every database constraint (string length, NUL), and the range limit on `by` and `launchSpeed` makes overflowing the `bigint` speed impossible in practice: it would take more than nine billion messages of one rocket. So in normal operation no statement fails and there is no overflow handling at all.

### D5a. Failed entries: isolate, don't stop

A statement can still fail because of a bug. Retrying the batch forever would stop all processing, because every processor would claim the same oldest entries again. So:

1. **Database unavailable** (connection error, timeout, `lock_timeout` or `statement_timeout` exceeded — the repository wraps them as `processor.ErrUnavailable`): roll back, mark nothing, wait with backoff, retry the batch.
2. **Any other error:** roll back, then process the same entries again **one per transaction**. An entry that fails on its own is marked `outcome = 'failed'` with the error as `reason` (in its own small transaction) and logged at ERROR; the others are applied normally.

`failed` is deliberately separate from `quarantined`: *quarantined* = the sender sent something invalid, *failed* = a valid message the system could not apply. Failed entries do not change state; they are reprocessed manually after a fix (automatic reprocessing is a non-goal).

*Alternatives considered:* bisecting the batch (fewer transactions, more code — failures are expected to be rare); a savepoint per upsert (one transaction, but the shared marking statement cannot be isolated that way); an attempts counter (still needs to find the bad entry first; transient errors are already handled by point 1); stopping the processor (all four would stop on the same entry); quarantining such entries (would mix sender errors with our own).

### D6. The processing loop

```
loop:
    n, err := ProcessBatch(batchSize)         // 1,000; claim, classify, fold, upsert, mark — one tx
    if err is ErrUnavailable:                 // database down: nothing was marked
        sleep postgres.Backoff(attempt++)     // 100 ms, doubling, at most 2 s; WARN with the delay
        continue
    attempt = 0
    if n == 0: sleep pollInterval (100 ms)    // otherwise continue at once
```

`ErrUnavailable` here is `processor.ErrUnavailable`, declared by the processor just as the handler declares its own; the repository wraps connection failures and timeouts with it. The backoff is the same function as for the initial connection. Without it, four processors would spin without pause during an outage — CPU, log noise, and a burst of connection attempts just as the database comes back. A fixed delay would be either too slow to recover after a short blip (2 s) or too aggressive during a long outage (100 ms); capping at 2 s means processing restarts at most 2 s after the database is back.

The processor owns its repository interface:

```go
// package processor
type Repository interface {
    // ProcessBatch claims up to limit unprocessed entries in one transaction,
    // passes them to fn, applies the resulting deltas and marks the entries
    // as fn decided. It returns the number of entries processed.
    ProcessBatch(ctx context.Context, limit int, fn func([]domain.InboxEntry) domain.BatchResult) (int, error)
}
```

### D7. Throughput

A batch costs one claim query (pending index), one marking `UPDATE … WHERE id = ANY`, at most one upsert per rocket (20) and one commit (~0.5 ms flush). At 4,000 msg/s a 10 ms loop iteration sees about 40 new entries, so the backlog stays at a few dozen; a cold backlog of 100,000 drains in batches of 1,000. Folding per rocket keeps row updates at ≤ 20 per batch whatever the batch size.

*To verify, not assumed:* the e2e test samples the oldest unprocessed entry's age during a default simulator run and how long after it entries stay unprocessed.

### D7a. Four processors

The stack runs four processor instances. Claims never overlap (`SKIP LOCKED`), so more instances add claim throughput and availability; with 20 rockets, several processors regularly update the same rocket in the same moment, which only serialises them on that row for one upsert (D4). Connections: 3 servers + 4 processors × 10 (pool default) = 70, below PostgreSQL's default `max_connections` of 100, leaving room for `migrate`, tests and `psql`.

### D8. Schema (`0002_processing.sql`)

```sql
alter table inbox
  add column processed_at timestamptz null,
  add column outcome      text null check (outcome in ('applied', 'quarantined', 'failed')),
  add column reason       text null,
  add check ((processed_at is null) = (outcome is null));
create index inbox_pending_idx on inbox (id) where processed_at is null;

create table rockets (
  channel                  text primary key,
  speed                    bigint not null default 0,
  rocket_type              text null,          -- null = not launched (message 1 not processed yet)
  launch_speed             bigint null,
  launched_at              timestamptz null,
  mission                  text null,
  mission_message_number   bigint not null default 0,
  explosion_reason         text null,          -- not null = exploded
  last_applied_message_number bigint not null default 0,
  last_applied_message_at  timestamptz null,
  updated_at               timestamptz not null default now()
);
```

Existing inbox rows become unprocessed and are handled by the first processor rounds.

### D9. Layering

```
cmd/rockets          processor subcommand: wiring, session limits, signals
internal/processor   loop (D6); owns Repository; no SQL, no HTTP
internal/domain      Classify, Delta, Fold, Merge — pure, stdlib only
internal/repository  implements processor.Repository next to handler.InboxRepository
internal/postgres    migration 0002, RequireSchema
```

`processor → domain`, `repository → processor, domain`, `cmd → all`. The processor serves no HTTP: nothing would call it (no container health check in the distroless image, nginx does not route to processors). Helpers follow the project rule (`helpers.go`, `helpers_test.go`; shared ones in `pkg/helpers`, shared test helpers in `internal/testdb`).

### D9a. Fixed settings

Batch size (1,000), poll interval (100 ms) and shutdown timeout (15 s) are constants in `internal/processor`, not flags: nothing needs to change them in operation, and tests set them through the package's own constructor. This removes option parsing, range validation and their tests; flags can be added later in minutes if ever needed.

### D10. Shutdown

The loop checks the context between batches; a running batch uses a context detached from the signal with a bounded timeout, so it commits or rolls back on its own. Then the pool closes — within the 15 s shutdown timeout, otherwise exit 1 (same contract as the server).

### D11. Test strategy

- **Domain** (no I/O): table tests per payload rule with reasons; per combine rule (deltas before launch, negative speed, late older mission, explosion before an earlier message, second explosion in a batch, not launched, last applied number and time); launch only as message 1 and message 1 only as launch; `by`/`launchSpeed` range; a property test — random message sets with invalid entries, split into random batches in random order, folded and merged, equal the in-order result (≥ 1,000 iterations).
- **Repository** (real database): claim skips locked and processed rows; marking and outcomes; every upsert rule checked against the database (launch after deltas, older mission ignored, …) including the same property test as the domain but merged in SQL; an explosion after an existing one is quarantined; an error inside a batch rolls back state and marks; a forced statement error marks exactly one entry `failed`.
- **Processor** (real database, in process): four processors on shuffled streams of several rockets equal in-order results; a failed batch re-run applies everything once; processing resumes after an outage via `testdb.NewProxy`.
- **Binary**: unmigrated database → status 1; `SIGTERM` during processing → exit 0, batch committed or rolled back.
- **End-to-end**:
  1. Freshness — during a default simulator run, sample the oldest unprocessed entry's age (≤ 1 s); nothing unprocessed 2 s after.
  2. Correctness — after a default run, recompute every rocket's expected state from the inbox (every entry is kept) in plain SQL and compare field by field.
  3. One processor stopped mid-run gives the same result.

## Risks / Trade-offs

- [Inbox grows without bound] → accepted for now (Non-Goals); the pending partial index keeps processing cost independent of history; retention or partitioning can be added later without changing the processor.
- [Marking creates dead tuples] → each entry is updated once; autovacuum defaults handle it at this scale; to be watched under sustained load.
- [State may combine messages that never coexisted while gaps exist] → accepted. Gaps are not tracked in the state; if needed later, they can be computed from the inbox, which keeps every entry.
- [Conflicting duplicates] → the first stored body wins at intake, as before.
- [Concurrent upserts on the same rocket] → serialised by the row lock for one statement; channel order prevents deadlocks.
- [Rows for rockets that were never launched] → speed changes before the launch create a not-launched row; such rows are found with `rocket_type IS NULL`, and the list endpoint of the next iteration shows only launched rockets.
- [A processor hangs holding locks] → session limits (D2) end its session within 30 s; others wait at most 5 s for a rocket row before backing off.
- [Messages after an explosion still change speed/mission] → chosen for order independence; never observed from the simulator.
- [Which of two explosions is kept depends on arrival] → only for a protocol violation; the second is quarantined, never applied (D3).
- [Speed overflow] → impossible in practice: `by` and `launchSpeed` are limited to 10⁹ (D5).
- [A bug makes a statement fail] → only the offending entry is marked `failed` (D5a); the rest of the batch is applied and processing continues.

## Migration Plan

`migrate` applies `0002_processing`; servers and processors require version 2 at startup. Entries already in the inbox are unprocessed and are handled by the first processor rounds.
