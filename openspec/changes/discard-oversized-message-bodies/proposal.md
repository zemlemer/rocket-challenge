# Proposal

## Why

An oversized request can currently leave an inbox record even if the upload ends early, because intake stops reading as soon as it detects the size limit. The processor quarantines oversized requests, so an inbox record of the size violation is sufficient for diagnosis.

## What Changes

- Read the request body to its end before saving an inbox entry, including after the body exceeds 1 MiB.
- For a fully received body over 1 MiB, commit an inbox entry with `truncated = true`, zero body bytes and no parsed metadata, then return `202`. The processor quarantines the entry for exceeding the size limit.
- Store a fully received body of exactly 1 MiB byte for byte. An interrupted upload creates no inbox entry at any size.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `message-intake` specifies when to persist an oversized request and when to acknowledge it.
- `database-schema` specifies the stored representation of newly received oversized requests and preserves historical records.
- `message-validation` specifies the quarantine outcome for the size marker.
- `deployment` specifies the oversized request result through nginx.

## Impact

Intake, processor classification, README and end-to-end verification need updates. Operators lose access to the retained prefix for newly received oversized requests. The existing database columns can represent the new record.
