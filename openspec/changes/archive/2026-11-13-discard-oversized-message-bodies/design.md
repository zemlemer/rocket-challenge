# Design

## Context

`readBounded` currently reads through the first byte beyond the storage limit. A direct HTTP request that declares 2 MiB and disconnects after 1,048,577 bytes has been observed to leave a truncated row in PostgreSQL. The inbox schema already supports a zero-length `bytea`, and the classifier checks `truncated` before identity or body.

## Goals

Keep buffering bounded in the HTTP handler and use the existing inbox transaction and processor quarantine path.

## Decisions

### Finish reading before persistence

Keep the 1 MiB + 1 byte bounded read. When it finds an extra byte, stream the rest to `io.Discard` under the existing HTTP read timeout. HTTP framing determines when the body is complete for both `Content-Length` and chunked requests. The persistence call starts only after the body reader reports successful completion. This keeps peak handler memory bounded by the limit plus read overhead and lets the existing error path handle disconnects, malformed framing and read timeouts.

### Store a size marker in the existing row

Use `truncated = true`, zero body bytes and `NULL` metadata columns. A genuine empty request has `truncated = false`. The existing `NOT NULL` body column supports both representations. Update the classifier's rejection reason to indicate that the body exceeded 1 MiB. The existing processor records the reason and counts the row as quarantined. Oversized entries count toward inbox totals and have no per-rocket identity.

## Risks / Trade-offs

- Reading the rest of an oversized upload can delay `202` and uses network bandwidth until the upload ends or the read deadline expires. nginx already buffers request bodies before forwarding them in the supplied deployment.
- Operators keep the receipt time and the quarantine reason for new oversized requests. Diagnosing the original payload requires access to the sender's copy.
- Repeated oversized deliveries can produce multiple size markers because the rows have no identity. Each marker is quarantined independently.

## Migration Plan

Deploy the updated server and processor with the current schema. Both historical rows with a retained prefix and new rows with zero body bytes use the same size flag, which both processor versions recognize. Existing inbox rows keep their stored values. Rolling back the server restores the prior prefix retention for future deliveries.
