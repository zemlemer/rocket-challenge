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
  `LISTEN/NOTIFY`. *Measured below (Processing): 100 ms polling and batches
  of 1,000 keep the oldest unprocessed entry under 600 ms.*
- **Not observed, still assumed:**
  - duplicates and redelivery after a non-`2xx` response;
  - gaps;
  - messages after an explosion.

  Tests have to construct these cases themselves.

## Processing (`add-message-processing`)

Default simulator run against the full stack (3 servers behind nginx, 4
processors, batch size 1,000, poll interval 100 ms), measured by the e2e test
`TestSimulatorDefaultRun`, which samples the oldest unprocessed inbox entry
every 100 ms:

| | Undisturbed | One processor stopped 5 s into the run |
|---|---|---|
| Intake | 100,000 messages in 27.6 s (≈ 3,600 msg/s) | 100,000 in 26.3 s (≈ 3,800 msg/s) |
| Oldest unprocessed entry during the run | 599 ms | 247 ms |
| Unprocessed after the run ends | 590 ms | 8 ms |
| State vs. recomputation from the inbox | identical for all 20 rockets | identical |

A later full `make test-e2e` run measured 14 ms / 5 ms (undisturbed) and
21 ms / 5 ms (one processor stopped). All runs stay within the targets of the
message-processing spec (≤ 1 s, ≤ 2 s). The spread — 14 ms to 599 ms for the
same load — points at the environment (Docker Desktop disk and CPU) rather than
the processor; the worst case so far leaves about 400 ms of margin.

## Explosion and event rotation (possible optimization, not implemented)

Two more runs on 2026-11-13, posting to a throwaway HTTP sink instead of the
stack: the default seed 444, and seed 7 with `--max-messages=300000`.

**Observed:**

- After `RocketExploded`, a channel sends nothing more. Seed 444: 6 of 20
  rockets exploded; seed 7: 19 of 20, and the simulator then stalled with one
  live rocket (stopped by hand after about 194,000 messages). No exploded
  rocket was replaced by a new one, and no channel restarted its numbering
  within a run.
- Channels are UUID version 5, so they are deterministic: 16 of the 20 channels
  of seed 444 and seed 7 are the same. A second run against the same database
  starts those channels again at `messageNumber` 1; intake treats the first
  part as duplicates and the rest is applied on top of the earlier rocket. Run
  the simulator once per database (`docker compose down -v` before a rerun).

**Assumption:** a rocket that has exploded sends no further events. This
holds in every run so far but is not promised by the challenge, and the
rocket-state spec still applies messages that arrive after an explosion.

**Optimization this would allow:** once a rocket has exploded with message
number N and all messages 1..N are processed without gaps, its inbox entries
could be rotated out (deleted or moved to an archive). The `rockets` row stays
and records N; a later delivery for that channel with a number ≤ N would be
marked as a duplicate by the processor, under the rocket's row lock, without
effect.

**Expected gain:** an inbox entry takes about 650 bytes including indexes
(30,000 entries = 19 MB, of which bodies 5.6 MB and the identity index
5.7 MB), so a default run needs about 65 MB. Exploded rockets account for 5 to
11% of the entries, so this rotation saves about that share. Most entries
belong to rockets that are still flying.

**Why it is not implemented:**

- It contradicts two approved requirements: processed entries stay in the
  inbox, and the inbox alone decides the answer to a redelivery. It needs a
  new outcome (`duplicate`) and a sealed-through number per rocket.
- If the assumption breaks (for example, a rerun on the same channels), the
  later messages would be dropped as duplicates instead of being applied.
- A missing message keeps the channel from being sealed, so the saving
  depends on the stream.
- Rotated entries can no longer be used to recompute state from the inbox,
  which the e2e test uses as its reference.

At the challenge's scale the space is not a problem, so the inbox keeps every
entry. A broader variant (a contiguous watermark per channel, compacting live
rockets too) would save more, with the same spec changes.
