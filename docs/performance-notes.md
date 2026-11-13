# Performance notes

Measurements of the compose stack (3 servers, 4 processors, PostgreSQL 16,
nginx) on macOS with Docker Desktop, 2026-11-13. The numbers are indicative,
not a benchmark: on a heavily loaded host throughput swung between 7 and
4,000 msg/s with stalls of several seconds; the figures below come from a
quiet host.

## Baseline

Default simulator run (100,000 messages, concurrency 3):

| Metric | Value |
|---|---|
| Throughput | about 3,300 msg/s, steady over the run |
| `POST /messages` at nginx | p50 1 ms, p99 2 ms, p99.9 4 ms, max 20 ms |
| Processing lag (stored → processed) | p50 2.8 ms, p99 15 ms, max 43 ms |
| Inbox drained after the run | 5–30 ms |

With `--concurrency-level 12`: about 4,600 msg/s, p99 3 ms. PostgreSQL alone
accepts 14,500 single-row inserts per second at 3 clients and 29,900 at 12
(pgbench in the stack's container), so intake is not limited by the database.

## Idle load

With an empty inbox each processor claims every 100 ms (`PollInterval` in
[internal/processor](../internal/processor/processor.go)): 40 transactions per
second for four processors. Per-process CPU time in the PostgreSQL container
over 30 idle seconds:

| Source | Share of one core |
|---|---|
| processor connections | 1.33% |
| checkpointer | 0.23% |
| other | 0.03% |
| total | 1.6% |

An empty claim reads one page of `inbox_pending_idx` and takes about 0.08 ms;
the cost is transaction overhead, not the query. Higher readings from
`docker stats` (5–19%) came right after load runs (autovacuum, checkpoint) and
from the measuring `psql` sessions themselves.

This is small and is accepted as is. The options below are recorded in case
idle cost starts to matter (many more processors, a database billed per
transaction).

## Possible optimizations

### Growing pause on an empty inbox

After consecutive empty batches, double the pause up to a cap (for example
100 → 200 → 400 ms) and reset it after the first non-empty batch, the same way
the processor already backs off on storage errors (`helpers.Backoff`).

- Gain: idle transactions fall by the cap's factor (×4 at 400 ms).
- Cost: the first message after a quiet period waits up to the cap; under load
  nothing changes, because batches are not empty.
- Effort: a few lines in `processor.Run` and unit tests with the fake clock.
  No new components, no new failure modes.

The cheapest option and the first to take if idle load ever needs reducing.

### Message broker (Kafka) instead of the inbox table

Servers produce each message to a topic, partitioned by `channel`, and
processors consume it in a consumer group. Consumers block on fetch, so an
idle system makes no database queries at all.

- Gain: no idle polling; intake no longer writes to PostgreSQL; consumers
  scale with partitions.
- What has to be rebuilt, because the inbox table provides it today:
  - **Durable acknowledgement**: `202` only after the broker confirms
    (`acks=all`, `min.insync.replicas` ≥ 2), with replication in the
    deployment.
  - **Deduplication**: the inbox's unique index on (`channel`,
    `message_number`) answers redeliveries at intake. With a broker,
    duplicates reach the consumer and need a processed-identities table or
    equivalent, written in the same transaction as the state.
  - **Exactly-once effect**: offsets and state live in different systems, so
    either store offsets in PostgreSQL with the state or rely on idempotent
    application.
  - **History and quarantine**: the inbox is the record of what arrived and
    what happened to it (`outcome`, `reason`); a topic with retention is not.
- Ordering: per-channel partitions give per-rocket order, but state is already
  computed independently of order, so this is not a gain here.
- Cost: a broker cluster to run and test, a second source of truth, and a
  redesign of intake and processing.

Justified only by load far beyond the challenge (sustained intake beyond what
one PostgreSQL accepts) or by other consumers of the same stream, not by idle
cost.

### LISTEN/NOTIFY wake-ups

Servers send `pg_notify` after storing an entry; processors `LISTEN` on a
dedicated connection and, after an empty batch, wait for a notification with
a long fallback timer (for example 1 s).

- Gain: idle claims fall about ×10, pickup after a quiet period stays fast.
- Constraint: `NOTIFY` inside the insert's transaction serializes intake
  commits. pgbench in the stack's PostgreSQL: insert + `NOTIFY` stays at about
  9,000 tps at both 3 and 12 clients, against 14,500 and 29,900 without
  `NOTIFY`. The signal must therefore be sent after commit and throttled per
  server (for example at most once per 10 ms).
- Cost: a signal sender in each server, a listener with reconnect logic in each
  processor, one extra connection per processor, and race handling between
  claims and signals. Correctness does not depend on signals, but the moving
  parts do.

More complexity than the growing pause for a similar gain at this scale.
