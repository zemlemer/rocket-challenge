# Design

## Context

Intake accepts any non-empty channel up to 255 bytes without NUL and stores a parsed identity. `domain.Classify` currently trusts that channel. The processor applies only entries that `Classify` accepts. See `proposal.md` for the motivation and the compatibility boundary.

## Goals / Non-Goals

**Goals:** Keep intake durable and idempotent while preventing newly processed messages with unsafe channel names from changing rocket state.

**Non-Goals:** Rewriting existing inbox entries or rocket rows; implementing GET routes in this change.

## Decisions

### D1. Validate at processing, after intake

Add a channel alphabet check to `domain.Classify` after the existing identity check and before payload decoding. Accept one to 255 ASCII bytes from `[A-Za-z0-9_-]`; the existing intake limit already bounds length. An invalid channel yields a rejection naming `metadata.channel`, so the processor's existing quarantine path marks it without state changes. Intake continues to acknowledge and deduplicate the original identity. Moving this check into intake would change approved metadata extraction and could make redelivery loop forever if it returned `4xx`.

### D2. Preserve historical data

No migration changes already processed entries or stored rocket rows. A deployment can therefore contain legacy rows with channels outside the new alphabet. The separate `add-rocket-query-api` change must exclude them from both reads when it introduces those routes; this change cannot implement that filtering because the routes do not exist yet. This dependency must be reviewed before either change is considered complete as a user-facing API.

## Risks / Trade-offs

- [Previously conforming messages become quarantined] → Mark the proposal breaking, keep their bodies and reasons in the inbox, and document the accepted alphabet.
- [Legacy state remains in storage] → Preserve it for audit; require the query API to hide it instead of deleting data.
- [A rule applied at processing can lag intake] → This is already the system's validation model; `202` means stored, not applied.

## Migration Plan

Deploy the processor with the new classifier. Pending entries are classified under the new rule. Rollback restores the old classifier; quarantined entries remain quarantined because automatic replay is outside the existing processing contract.
