# Design

## Context

The processor stores current state in `rockets`; the server currently exposes only message intake and liveness. The table contains rows before launch (`rocket_type` is `NULL`) when later-numbered messages arrive first. It may also contain rows whose channel breaks the channel rule, created before restrict-rocket-channels made processing quarantine them. See `proposal.md` for the API motivation.

## Goals / Non-Goals

**Goals:** Keep reads separate from message processing and return a consistent public representation from both query endpoints.

**Non-Goals:** Waiting for pending messages during reads; pagination in this change; deleting or repairing stored rocket rows; CORS headers for a browser dashboard on another origin; HTTP caching headers (`Cache-Control`, `ETag`).

## Decisions

### D1. Show launched and exploded rockets

A rocket is visible when its row has `rocket_type IS NOT NULL OR explosion_reason IS NOT NULL`: the schema's markers of a processed launch and of a processed explosion. Processing writes an explosion with an `UPDATE` of `explosion_reason` only and never clears `rocket_type`, so a launched rocket stays visible after it explodes (verified in `internal/repository/processing.go`). A rocket with only speed or mission changes returns `404` and is not listed until its launch or explosion is processed.

An explosion is terminal, so the fact that a rocket exploded matters to a dashboard even when its launch message is late or was quarantined. Such a rocket is shown with what is known: `type`, `launchSpeed` and `launchedAt` are `null`, `mission` is `null` unless a mission change arrived, and `speed` holds the increments applied so far without the launch speed. When the launch arrives later, these fields fill in and `speed` grows by the launch speed. Rockets with only speed or mission changes stay hidden: they carry no fact a client can rely on yet.

Alternative considered: launched rockets only. Simpler (no `null` launch fields, no `NULL` ordering), but an exploded rocket with a late or quarantined launch would be invisible.

For an `active` rocket the launch fields are always present: active implies launched, launch payload rules require non-empty `type` and `mission`, and the launch is message 1, so it always sets the mission.

### D2. Read committed processed state

The read API queries the `rockets` projection. It does not inspect or wait for pending inbox entries. A `202` from intake confirms persistence, so a subsequent GET can briefly show older state. Waiting for processing would couple read latency and availability to the processor and would require a consistency token or deadline contract.

Each response comes from one `SELECT`, so a list is a single snapshot: it never mixes a rocket's state from two moments. Plain reads take no row locks and therefore never block the processor's `FOR UPDATE SKIP LOCKED` batches.

### D3. One representation and selectable sorting

Both endpoints use the same rocket representation:

| JSON field | Source | Notes |
|---|---|---|
| `channel` | `channel` | |
| `type` | `rocket_type` | `null` before launch |
| `status` | `explosion_reason` | `active` when absent, `exploded` when present |
| `speed` | `speed` | integer, may be negative; without launch speed before launch |
| `launchSpeed` | `launch_speed` | integer; `null` before launch |
| `mission` | `mission` | `null` until a launch or mission change |
| `launchedAt` | `launched_at` | message time of the launch; `null` before launch |
| `explosionReason` | `explosion_reason` | `null` while active |
| `lastMessageNumber` | `last_applied_message_number` | highest applied number, not a guarantee that all lower ones were applied |
| `lastMessageAt` | `last_applied_message_at` | message time of that message |

Times are RFC 3339 in UTC: PostgreSQL `timestamptz` keeps the instant but not the sender's offset, and only to the microsecond. Verified on the compose stack: intake passes `time.Time` to pgx, whose binary encoding truncates to microseconds (`.123456789` is stored as `.123456`; PostgreSQL itself would round a text value to `.123457`), so a nanosecond fraction from the sender comes back truncated. `jsonTime` also truncates, so values that bypass the database behave the same. They are the sender's message times: intake and processing accept any RFC 3339 value, so they may lie in the future, and `lastMessageAt` may be earlier than `launchedAt`. `mission_message_number` and `updated_at` are processing internals and are not exposed.

Times are formatted with `time.Format(time.RFC3339Nano)` on the UTC value, not with `time.Time.MarshalJSON`. Verified with Go 1.27: a message time such as `9999-12-31T23:30:00-01:00` is valid RFC 3339 but falls in year 10000 in UTC, and `MarshalJSON` then fails with `year outside of range [0,9999]`. One such rocket, which any sender can create, would turn every `GET /rockets` into `500`. `Format` writes `10000-01-01T00:30:00Z` (and `-0001-…` for negative years) instead; those values are outside strict RFC 3339 but keep the response intact.

The list is wrapped as `{"rockets": [...]}` so that a later pagination change can add fields without breaking clients.

`?sort=` accepts `channel` (default), `type`, `mission`, `speed`, `status` or `launchedAt`; `?order=` accepts `asc` (default) or `desc`. `type` is included because the challenge lists it first among the state fields. `status` orders `active` before `exploded` in ascending order. `type`, `mission` and `launchedAt` can be `NULL` for an exploded rocket without a launch; those rows sort last in either direction (`NULLS LAST`), so unknown values never lead a list. Text fields compare byte-wise (`COLLATE "C"`), so the order does not depend on the database locale. Ascending `channel` is the secondary key in either direction, so repeated queries over unchanged state have a stable order. An unknown, wrongly cased, empty or repeated `sort`/`order` value gets `400`; other query parameters are ignored. The query string is parsed with `url.ParseQuery` and a parse error gets `400`: verified with Go 1.27, `r.URL.Query()` silently drops a malformed pair such as `sort=%zz`, which would answer a broken request with the default order. Sorting happens in SQL from a fixed allow-list of `ORDER BY` clauses, never from interpolated input.

### D4. Full list now; pagination later if needed

`GET /rockets` returns every visible rocket in one response. The challenge asks for all rockets, and a default simulator run produced 20 rockets (verified by the simulator E2E test). Pagination would add client navigation, page-boundary and concurrent-update semantics without a present scale need. If the number of rockets grows enough to make full-list reads expensive, add pagination in a separate change with its own API contract and tests.

No index is added: `channel` is the primary key for single reads, and sorting a list of this size in memory is cheap.

### D5. Hide rows with an invalid channel

The channel rule is applied only in Go, with the same domain function that processing uses:

- `GET /rockets/{channel}` checks the path value before querying and answers `404` without a database round trip.
- `GET /rockets` reads launched or exploded rows in the requested order and drops rows whose channel breaks the rule before building the response. Removing rows keeps the order of the rest.

Such rows cannot be created any more; they are hidden, not deleted, so stored state is left as it is and the change needs no migration.

Alternatives considered:
- A regular expression in SQL (`channel ~ '^[A-Za-z0-9_-]{1,255}$'`). It would keep hidden rows out of the result set, but the rule would then exist in two regex engines that differ on edge cases (Postgres counts characters, Go bytes; bracket ranges and `$` before a trailing newline behave differently between engines), and a cross-check test would be needed to keep them in line.
- A migration deleting those rows. It would keep the queries free of the rule but is irreversible and turns the approved "hide" into "delete".

Cost: the list query also reads the hidden historical rows, which are few and no longer grow.

### D6. Errors

- Unknown, neither launched nor exploded, or invalid-format channel: `404 {"error": "rocket not found"}`. The client cannot act on the difference, and the answer does not reveal whether a hidden row exists.
- Invalid `sort` or `order`: `400` naming the parameter and the accepted values. A query string `url.ParseQuery` cannot parse gets `400` with its error: verified with Go 1.27, this covers a bad `%` escape and `;` used as a separator (`invalid semicolon separator in query`), so the message names the real cause. Unlike `POST /messages`, a query is the client's own request and can be corrected.
- Database unavailable: `503`, other failures `500`, as in message intake. Each read runs under a 5 s context deadline; an expired deadline also gets `503`, so a stuck database does not hold requests until the 30 s write timeout.
- A client that disconnects cancels the request context and the query. That is logged at debug level, not as an error, and no response is attempted.
- Wrong method and unknown paths keep the existing `405`/`404` behavior of the server.

### D7. Routing edge cases

Routes use the standard `ServeMux` patterns `GET /rockets` and `GET /rockets/{channel}`. Verified with Go 1.27:

- A `GET` pattern also serves `HEAD`, so `HEAD` is allowed explicitly and other methods get `405` with `Allow: GET, HEAD`, as `/healthz` already does.
- `ServeMux` redirects paths with `.`, `..` or empty segments to their cleaned form (`307`); `/rockets/../healthz` goes to `/healthz`. This is accepted as is rather than overridden.
- `{channel}` is percent-decoded: `/rockets/a%2Fb` gives `a/b` and `/rockets/%E2%82%AC` gives `€`; both break the channel rule and get `404`.

The channel rule (ASCII letters, digits, `-`, `_`) is enough to make the path unambiguous: no valid channel needs percent-encoding or can be `.`/`..`, so each rocket has one canonical URL, and anything else decodes to a channel outside the rule. Percent-encoding an allowed character (`/rockets/c%31`) decodes to `c1` and reaches the same rocket; RFC 3986 treats these forms as equivalent, so this is harmless.

### D8. No nginx retry of read `503`

The `location /` block retries `error timeout http_502 http_503` on another replica, which suits intake: a replica that cannot store a message is worth bypassing. For reads a `503` comes from the shared database (unavailable or slower than the 5 s read timeout), so another replica cannot do better. A separate `location /rockets` keeps the same proxy settings but retries only `error timeout http_502`.

Verified on the compose stack with PostgreSQL paused: with `http_503` in the read location, `GET /rockets` answered `503` after 15.0 s (three attempts); without it, after one 5 s attempt. In both cases `POST /messages` got `202` on every replica right after the database was unpaused, so the retry did not take replicas out of rotation; the gain is latency and database load, not intake availability.

## Risks / Trade-offs

- [Full-list size grows with rocket count, and any sender can create a rocket by posting a launch on a new channel] → Keep the initial contract simple; measure list size and latency before proposing pagination. The list is unbounded; recorded as a limitation.
- [GET may lag a successful POST] → Document that reads reflect committed processing rather than intake acknowledgements.
- [A rocket whose message 1 was quarantined becomes visible only if it explodes; until then its later messages change stored state invisibly] → Accepted consequence of D1; the inbox shows the quarantine reason.
- [An exploded rocket shown before its launch has a `speed` without the launch speed, which jumps when the launch arrives] → `launchSpeed: null` marks this state; documented in the README.
- [`lastMessageNumber` can be read as "everything up to N is applied"] → Document that it is the highest applied number, with gaps possible.
- [Sorting ties could be unstable] → Use channel as a deterministic secondary key.
- [`speed` can exceed 2^53 after millions of increases of up to 10^9; JSON stays exact, but a JavaScript client loses precision] → Recorded as a limitation; not changed in this change.
- [Times come from the sender's clock, so sorting by `launchedAt` follows those clocks] → Documented; not corrected.
- [The general nginx location retries `503` on the next replica, so a read hitting a slow database would wait for each replica's 5 s timeout and repeat the query] → D8: a separate `location /rockets` does not retry `503`.
- [Hidden historical rows are read from the database on every list request] → They are few and cannot grow any more (D5).

## Migration Plan

Add read routes and read-only repository queries to the existing server. No database migration or processor deployment change is needed. Removing the routes rolls back this API without changing stored state.
