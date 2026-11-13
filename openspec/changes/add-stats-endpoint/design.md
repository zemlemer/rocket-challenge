# Design

## Context

The inbox already records every accepted message with `received_at`, its identity (`channel`, `message_number`, unique per identity through `inbox_identity_uq`), and after processing `processed_at`, `outcome` and `reason`. Unprocessed entries are indexed by `inbox_pending_idx`. Rocket reads follow a fixed pattern: a repository interface declared in `internal/handler`, a pgx implementation in `internal/repository` whose errors go through `classify` (outages become `ErrUnavailable`), a 5 s `readTimeout`, and `readFailed` for the 503/500/client-gone split. The channel rule (`domain.ValidChannel`) is applied by handlers, not by SQL (rocket-query design D5). Three server replicas sit behind nginx round-robin; four processors write the inbox concurrently.

## Goals / Non-Goals

**Goals:** one cheap-enough read that answers "did everything arrive, is it processed, is every rocket complete" consistently from any replica, reusing the read-API patterns unchanged.

**Non-Goals:** a stored or incremental counter, history over time (rates, throughput), per-process numbers (HTTP latency, pool usage), paging the `rockets` array.

## Decisions

### D1. Derive everything from the inbox at request time

Two aggregate queries over `inbox`: totals plus oldest pending, and a `group by channel` over entries with identity. No new tables or counters, so the processors' write path and the migrations stay untouched, and the report can never disagree with the data it summarises. Alternatives: counters maintained by processors (one more write per batch, a new source of truth that can drift) or Prometheus per process (needs HTTP in the processor, which processor-runtime forbids, and aggregation across replicas behind round-robin). Rejected for this scope.

The oldest-pending part uses `inbox_pending_idx` (`min(id)` among pending, then its `received_at`; ids grow with arrival so this is the oldest received). The per-channel part scans every entry with identity; see Risks.

### D2. One snapshot per report

Both queries run in one `REPEATABLE READ READ ONLY` transaction, so totals and per-rocket rows describe the same committed state, and `asOf` is the transaction's `now()`. Plain selects take no row locks, so processors (`FOR UPDATE SKIP LOCKED`, updates of `inbox`) are never blocked. Alternative: a single statement with CTEs gives the same snapshot under the default isolation but returns two shapes through one row set; two queries in one transaction are simpler to read and scan.

### D3. Database time for `asOf` and the age

The age of the oldest pending entry is `now() - received_at`, computed in SQL. `received_at` is set by the database's clock, so using the same clock avoids skew between the server container and PostgreSQL. The age is rounded to milliseconds.

### D4. Counts in the repository, rules in the handler

The repository returns raw numbers per channel (`highest`, `received` counting only numbers from 1 to `highest`, `pending`, outcome counts) for every channel. The handler filters with `domain.ValidChannel`, computes `missing = highest - received` and `complete`, and renders JSON, as the rocket list does with the channel rule. `received` per rocket counts entries with numbers in `1..highest`; since identities are unique this equals the number of distinct numbers, so duplicates cannot hide a gap. Entries with a number below 1 (quarantined by validation) are not counted per rocket but are counted in the totals.

Handler types: `handler.Stats` with a `StatsRepository` interface (`Stats(ctx) (StatsSnapshot, error)`), implemented by `repository.Stats`. A new route `GET /stats` with the existing `methodNotAllowed("GET, HEAD")` fallback.

### D5. Response shape

```json
{
  "asOf": "2026-11-13T10:00:00.123456Z",
  "inbox": {
    "received": 100000, "pending": 0,
    "applied": 99990, "quarantined": 10, "failed": 0,
    "oldestPendingAt": null, "oldestPendingAgeSeconds": 0
  },
  "rockets": [
    { "channel": "193270a9-...", "highestMessageNumber": 6498, "received": 6498,
      "missing": 0, "pending": 0, "applied": 6497, "quarantined": 1, "failed": 0,
      "complete": true }
  ]
}
```

`highestMessageNumber` is named apart from the rocket API's `lastMessageNumber`, which means the newest *applied* message. Times use the existing `jsonTime`. No top-level "all complete" flag: a reviewer reads `pending` and the per-rocket flags; a summary flag can be added later without breaking clients.

### D6. nginx: same retry rule as `/rockets`

`/stats` would otherwise fall under `location /`, which retries `503` (for intake). A `location /stats` with `proxy_next_upstream error timeout http_502` makes a database outage answer after one 5 s read timeout instead of up to three.

## Risks / Trade-offs

- [Full inbox scan per request; cost grows with the inbox] → At challenge scale (100,000 entries) this is expected to take tens of milliseconds (assumption, to be measured in the integration tests' timing or a manual run). The 5 s read timeout bounds it with a `503`; documented as a limitation. A larger deployment would need per-channel counters.
- [`watch -n1` from a reviewer adds load during a simulator run] → One scan per second on a read-only snapshot; it takes no locks and the processors do not wait for it. Verified only by the concurrency scenario, not by a load test.
- [`rockets` grows with the number of channels, unpaged] → 20 rockets in the simulator; the response is an object, so paging can be added later.
- [A long-running snapshot keeps old row versions alive] → Bounded by the 5 s timeout.
- [Missing numbers above `highestMessageNumber` are invisible] → A lost trailing message cannot be detected without knowing the expected last number; stated in README.

## Migration Plan

No schema change. Deploy the new server image; rollback removes the route. Processors are unaffected.
