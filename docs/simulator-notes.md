# Simulator notes

Observed behavior of the provided simulator (`rockets` 0.1.0 from the challenge
ZIP, macOS arm64), recorded from the bodies stored in the inbox by
`add-message-intake`. Two runs on 2026-11-12, both with the default options:

```sh
./rockets launch "http://localhost:8088/messages"
```

## Default options

`launch --help` lists the defaults the graders use:

| Option | Default |
|---|---|
| `--concurrency-level` | 3 |
| `--max-messages` | 100000 |
| `--message-delay` | none (no pause) |
| `--seed` | 444 |

So the default run is **100,000 messages, 3 concurrent senders, no delay**.
It took 26 s (≈ 3,800 messages/s) against the compose stack, not the
2 messages/s of the README's example (`--message-delay=500ms
--concurrency-level=1`).

## Results

| | Undisturbed run | One server replica stopped for 6 s mid-run |
|---|---|---|
| Requests | 100,000 | 100,000 |
| Responses | all `202` | all `202` (nginx retried 1 request on another replica) |
| Inbox entries | 100,000 | 100,000 |
| Entries without identity | 0 | 0 |
| Truncated entries | 0 | 0 |
| Distinct identities | 100,000 | 100,000 |

## Message stream

- **Channels:** 20 rockets, one `RocketLaunched` each.
- **Numbering:** every channel starts at `messageNumber` 1 and is contiguous.
  No gaps were observed. The highest number in a channel was 6,498.
- **Duplicates:** none. Every request got `2xx` and the simulator never resent
  a message. Redelivery after a non-`2xx` response was not exercised in these
  runs and is still an assumption.
- **Order:** 13 of 100,000 messages arrived after a higher-numbered message of
  the same channel, at most 2 positions late. This is consistent with three
  concurrent senders. Out-of-order arrival is real but shallow at the defaults.
- **Explosion:** 6 rockets exploded. `RocketExploded` was always the last
  message of its channel; no messages followed it.
- **Speed:** with messages applied in number order, no rocket's speed ever went
  below 0 (the minimum was 500).

| `messageType` | Count |
|---|---|
| `RocketSpeedDecreased` | 68,168 |
| `RocketSpeedIncreased` | 30,271 |
| `RocketMissionChanged` | 1,535 |
| `RocketLaunched` | 20 |
| `RocketExploded` | 6 |

## Format

- The top-level keys are exactly `metadata` and `message`. The metadata keys are
  exactly `channel`, `messageNumber`, `messageTime` and `messageType`. No other
  fields appeared.
- `channel` is a UUID string.
- All numbers are JSON integers: `messageNumber`, `launchSpeed` (500 in every
  launch) and `by` (500 to 10,000). No fractions or exponents appeared.
- `messageTime` is RFC 3339 with a numeric offset and 2 to 6 fractional digits,
  e.g. `2026-11-12T19:16:03.853369+01:00`. It is the send time, not a
  historical date.
- `RocketExploded.reason` values seen: `FROZEN_O_RINGS`,
  `PRESSURE_VESSEL_FAILURE`, `SYSTEM_MALFUNCTION`, `UNKNOWN_OBJECT_COLLISION`.
- Bodies are 187 to 237 bytes, far below the 1 MiB truncation limit.

## Consequences for the next change (`add-rocket-state-tracking`)

- **Assumptions confirmed:**
  - `messageNumber` starts at 1 and is a positive integer;
  - the payload shapes and integer types match the `message-format` spec;
  - every real message carries an identity.
- **Throughput:** the processor must keep up with about 4,000 messages/s from 20
  channels. The design sized it for 2 messages/s (200 ms polling, 500 rows per
  channel batch). With the default load, a latency target of 1 s needs this
  checked again, and probably larger batches or polling woken by
  `LISTEN/NOTIFY`.
- **Not observed, still assumed:**
  - duplicates and redelivery after a non-`2xx` response;
  - gaps;
  - messages after an explosion.

  Tests have to construct these cases themselves.
