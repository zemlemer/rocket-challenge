## MODIFIED Requirements

### Requirement: Inbox storage
Each inbox entry SHALL store an increasing unique ID reflecting insertion order, a receipt time, body bytes, a boolean `truncated` flag and any parsed metadata (identity, `messageType` and `messageTime` as a timestamp with time zone). For new entries, `truncated` SHALL indicate a fully received body larger than 1 MiB. Such entries SHALL have zero body bytes and all metadata columns `NULL`. Historical entries SHALL retain their stored bytes and metadata.

#### Scenario: Entry with identity
- **WHEN** a body is stored with channel `c1`, message number 5, a message time, message type `RocketLaunched` and truncation flag `false`
- **THEN** the inbox holds one entry with that body, metadata, flag and a receipt time

#### Scenario: Entry without identity
- **WHEN** a body that is not valid JSON is stored without identity
- **THEN** the inbox holds it with empty metadata

#### Scenario: Oversized entry and empty request
- **WHEN** intake records a fully received oversized request and a separate empty request
- **THEN** both entries have zero body bytes and all metadata columns `NULL`, and only the oversized entry has `truncated = true`

#### Scenario: Historical truncated body
- **WHEN** the system is upgraded with an existing `truncated = true` row containing a stored prefix
- **THEN** the row and its prefix remain in the inbox
