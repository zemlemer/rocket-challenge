# Tasks

## 1. Intake

- [x] 1.1 Finish reading oversized request bodies before saving a record with the existing size flag, zero body bytes and no metadata. Document the `202` response and the size boundary in `README.md`. Run the handler tests for exactly 1 MiB, 1 MiB + 1 byte, an empty body and a read error after exceeding the limit. Run the server tests against PostgreSQL for an oversized disconnect and complete redelivery, and for complete and incomplete chunked uploads across the limit.

## 2. Processing

- [x] 2.1 Report an exceeded body limit as the quarantine reason. Verify a new zero-byte size record and a historical truncated row are both quarantined, preserve their stored bodies and change no rocket state. Run the domain and PostgreSQL processing tests.

## 3. Deployment verification

- [x] 3.1 Update the nginx e2e size test to verify `202` and a new zero-byte inbox row with `truncated = true` and `NULL` metadata. Run the full e2e suite, including the default challenge simulator, on a fresh Compose database.
