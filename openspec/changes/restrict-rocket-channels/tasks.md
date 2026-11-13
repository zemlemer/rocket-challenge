# Tasks

## 1. Channel validation

- [x] 1.1 Add the `[A-Za-z0-9_-]` channel check to `domain.Classify` before payload decoding, keeping the existing intake contract; verify with table tests for allowed letters, digits, `-`, `_`, 255 bytes, and rejected `/`, `.`, `%`, `?`, `#`, space, Unicode and mixed strings under `go test -race ./internal/domain`.
- [x] 1.2 Add an integration test proving an invalid-channel launch is accepted with `202`, stored once on redelivery, quarantined with a channel reason and never creates rocket state; prove a valid UUID launch still applies and an out-of-order stream with a valid channel retains its prior result; run the affected integration tests with the database.
- [x] 1.3 Document the processing-stage channel rule and historical-row limitation in `README.md`; run `make test`, `make test-integration`, `make lint`, and the provided simulator through `make test-e2e`, recording any remaining limitation.

## Workflow follow-up

- Review the specification before starting implementation, then verify and archive this change.
- Ensure `add-rocket-query-api` excludes historical rows with invalid channels before its GET routes are implemented.
