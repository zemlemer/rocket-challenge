# Tasks

## 1. Oldest pending by receive time

- [x] 1.1 Add `min(received_at) filter (where processed_at is null)` to `statsTotals`, scan it into `OldestPendingAt`, and remove `statsOldestPending` and its query. Add a test inserting two entries whose id order is the reverse of their `received_at` order. Verify with `go test ./internal/repository/intake` against the test database and `make test-integration`.
