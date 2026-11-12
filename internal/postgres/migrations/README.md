# Schema migrations

Forward-only SQL migrations, embedded into the `rockets` executable and applied
by `rockets migrate` in version order.

- File name: `NNNN_name.sql` — `NNNN` is the version (positive integer, unique),
  `name` uses lowercase letters, digits and underscores, e.g. `0001_inbox.sql`.
- Each file runs in its own transaction together with its `schema_migrations`
  record; a file may contain several statements.
- Never edit a migration that has been applied anywhere; add a new one instead.
- Files other than `*.sql` (like this README) are ignored.
