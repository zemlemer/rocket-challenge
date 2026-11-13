# Proposal

## Why

Intake currently accepts channel names containing URL-significant characters. Restricting channels to a simple alphabet makes the future rocket lookup path unambiguous while preserving durable acknowledgement of every delivered message.

## What Changes

- **BREAKING:** A message whose channel contains anything outside ASCII letters, digits, `-`, and `_` is no longer conforming and is quarantined without changing rocket state. The existing non-empty, 255-byte maximum still applies.
- `POST /messages` continues to store and acknowledge such messages with `202`; intake metadata extraction and deduplication do not change.
- Existing rocket rows are preserved. Filtering legacy rows in the future GET API belongs to `add-rocket-query-api`; this change does not implement GET routes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `message-validation`: require a URL-safe channel alphabet before a message may change rocket state.

## Impact

- Changes domain classification and its tests. No database migration, intake response change, or processor scheduling change.
- The simulator's observed UUID channels satisfy the new rule; this is an observation, not a guarantee about every possible sender.
