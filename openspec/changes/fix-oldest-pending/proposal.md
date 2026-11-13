# Proposal

## Why

`GET /stats` takes `oldestPendingAt` from the pending entry with the smallest `id` (`order by id limit 1`), assuming ids grow with arrival. They do not strictly: `id` comes from the sequence when the row is built, `received_at` is `now()`, the start of the inserting transaction. Concurrent inserts, and an insert that waits on the identity index, can commit an entry with a larger id and an earlier `received_at`, so the report can understate the processing lag. The lookup is also a second statement although the totals query already reads every `inbox` row.

## What Changes

- The totals query computes `min(received_at) filter (where processed_at is null)` alongside its counts, so `oldestPendingAt` is the earliest receive time among pending entries.
- The separate `statsOldestPending` query by id is removed.
- No JSON or HTTP change; the requirement is sharpened to say "earliest receive time" rather than "oldest entry".

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `processing-stats`: requirement "Processing lag" states that `oldestPendingAt` is the earliest `received_at` among unprocessed entries, independent of their storage order.

## Impact

- Code: `internal/repository/intake/stats.go` (one query less).
- Tests: `internal/repository/intake/stats_test.go` gains a test with two pending entries whose id order is the reverse of their receive order.
- Performance: none measurable; the totals query already scans `inbox`, the aggregate adds one comparison per pending row. `inbox_pending_idx` is still used by the processors.
