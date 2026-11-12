# Tasks

## 1. Project setup

- [ ] 1.1 Initialise the Go module, `cmd/rockets`, `internal/postgres`, `.gitignore` and a `Makefile` with `build`, `test`, `test-integration`, `lint`, `db-up`, `db-down` targets; verify `go build ./...` and `go vet ./...` succeed
- [ ] 1.2 Add the integration-test helper (D8): skip with a clear message when `TEST_DATABASE_URL` is unset, otherwise create and drop a fresh database per test; verify with a trivial test that runs under `make test-integration` and is skipped under plain `go test ./...`

## 2. Executable and CLI

- [ ] 2.1 Add kong and the root CLI (D1): global options with env tags, `slog` setup (D7), `migrate` subcommand stub, exit-status mapping; verify with `os/exec` tests for `--help` (lists `migrate`, status 0), unknown mode (status 2), missing `DATABASE_URL` (status 2, message names both forms), flag overriding env
- [ ] 2.2 Implement database connection with retry and backoff (D5); verify with an integration test against an unreachable address (status 1 after the timeout, clear message) and a unit test of the backoff schedule

## 3. Migration mechanism

- [ ] 3.1 Implement the runner over an `fs.FS` (D2, D4): versions from file names, ordered application, one transaction per migration with its `schema_migrations` record; verify with integration tests for an empty database, a second migration added later, a second run applying nothing, and a broken migration leaving the previous version intact with an error naming the file
- [ ] 3.2 Add the session advisory lock around a run (D3); verify with an integration test where three concurrent runs on an empty database all succeed and each version is recorded once, under `go test -race`
- [ ] 3.3 Wire the embedded production set (`//go:embed migrations`, currently only its `README.md`) into `rockets migrate`; verify `rockets migrate` against an empty database creates `schema_migrations`, logs version 0 as up to date and exits 0, and a second run does the same

## 4. Image and compose

- [ ] 4.1 Add the multi-stage `Dockerfile` (D6); verify `docker build` succeeds and `docker run <image> migrate --help` prints help and exits 0
- [ ] 4.2 Add `compose.yaml` with `postgres` and the one-shot `migrate` service (D6); verify `docker compose up --build` ends with `migrate` exited 0 and `schema_migrations` present, a second `docker compose up` reports "up to date", and data survives `docker compose down` / `up` without `-v`
- [ ] 4.3 Add a short `README.md` section on running migrations (compose and local) and on `TEST_DATABASE_URL`; verify every documented command runs as written

## Workflow follow-up

- Review with `/opsx:verify`, then archive with `/opsx:archive` before starting the next iteration.
