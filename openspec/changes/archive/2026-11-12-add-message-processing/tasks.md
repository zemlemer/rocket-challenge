# Tasks

## 1. Schema

- [x] 1.1 Add migration `0002_processing.sql` (D8): processing columns and pending index on `inbox`, `rockets` table; raise the version required by `server` and `processor`; verify with integration tests that migrating a database holding inbox entries keeps them as unprocessed, the outcome check and the processed/outcome consistency check reject bad rows, and re-running is a no-op

## 2. Domain

- [x] 2.1 Add `internal/domain` with `InboxEntry`, typed messages and `Classify` (D5; spec message-validation); verify with table tests for every message type, every payload rule (missing, wrong type, negative, above 1,000,000,000, fraction, exponent, NUL, 256-byte string, empty string), missing metadata, unknown type, `RocketLaunched` with a number other than 1, number 1 that is not a launch, no `message`, `message` that is `null`/array/string/number, `{}` for every type, a required field that is `null`, truncated and invalid JSON, each with its expected reason
- [x] 2.2 Add `Delta`, `Fold` and `Merge` with the combine rules (D3, D4; spec rocket-state); verify with table tests: deltas before launch, negative speed, late older mission, explosion before an earlier message, a second explosion in the same batch quarantined, not launched, `last_applied_message_number`/`last_applied_message_at` from applied entries only
- [x] 2.3 Add a property test: random message sets with non-conforming entries, split into random batches in random order, folded and merged, equal folding them once in order; verify ≥ 1,000 iterations pass under `go test -race`

## 3. Processor

- [x] 3.1 Add `internal/processor` with the `Repository` interface, the fixed settings (D9a) and the loop (D6, D10): process a batch, sleep only after an empty batch, back off on `ErrUnavailable` (100 ms doubling to 2 s, reset after a successful batch), stop between batches on cancellation; verify with unit tests against a fake repository and an injected clock, including the delay sequence and its reset

## 4. Repository

- [x] 4.1 Implement `ProcessBatch` in `internal/repository` (D2–D4): claim with `FOR UPDATE SKIP LOCKED`, upserts in channel order, marking with outcome and reason, one transaction; unavailable errors retried, any other error isolated by re-running the batch one entry per transaction and marking the offender `failed` (D5a); verify with integration tests: locked and processed rows are skipped, every upsert rule against the database, the domain property test merged in SQL, a statement error forced by a test-only constraint marks exactly one entry `failed` and applies the rest, an explosion after an existing one is quarantined while the reason stays
- [x] 4.2 Add in-process integration tests in `test/integration` (spec message-processing): four processors on shuffled streams of several rockets equal in-order results; a failed batch re-run applies everything once; processing resumes after an outage via `testdb.NewProxy`; a transaction that claims a batch and then stops (session limits shortened for the test) loses its claim and locks, another processor applies those entries once, and a processor blocked on a locked rocket row backs off without marking; a redelivery of a processed message is dropped at intake and leaves state unchanged; verify all pass under `go test -race`

## 5. Processor mode and deployment

- [x] 5.1 Add `rockets processor` (no mode-specific flags; session limits and TCP keepalive on its pool (D2); schema check; graceful shutdown; spec processor-runtime); verify with binary tests: unmigrated database → status 1 naming `rockets migrate`; a started processor processes entries; `SIGTERM` during processing → exit 0 with the batch committed or rolled back
- [x] 5.2 Add `processor` × 4 to `compose.yaml` (after postgres healthy and migrate, restart, no ports, stop grace period; spec deployment: Processor replicas); verify `docker compose up --build` starts both, plus an e2e check of the compose dependencies

## 6. End-to-end verification

- [x] 6.1 E2E freshness test: during a default simulator run sample the oldest unprocessed entry's age every 100 ms (≤ 1 s) and require none unprocessed 2 s after the run (spec: Throughput and freshness); record the measured numbers in `docs/simulator-notes.md`
- [x] 6.2 E2E correctness test: after a default run recompute every rocket's expected state from the inbox in plain SQL and compare field by field; repeat with one processor stopped mid-run
- [x] 6.3 Update `README.md` (processor mode and options, how state is computed, processing status and quarantine in the inbox, how to inspect them); run `make test`, `make test-integration`, `make test-e2e` with the simulator and `make lint`; verify all pass and every documented command runs as written

## Workflow follow-up

- Review with `/opsx:verify`, then archive with `/opsx:archive` and merge into `main`.
