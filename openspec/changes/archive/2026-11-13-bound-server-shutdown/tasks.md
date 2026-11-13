# Tasks

## 1. Bounded unclean shutdown

- [x] 1.1 Add the `Work` context to `handler.Messages` and derive the insert context from it (design D1); nil means `context.Background()`. Verify with `go test ./internal/handler`.
- [x] 1.2 Give `server.Run` an `abort func()` called before `srv.Close()` on timeout, and export `server.ErrUncleanShutdown` (design D2). Verify with `go test ./internal/server`.
- [x] 1.3 In `ServerCmd.Run` create the server-lifetime context, pass it to the handler and `abort` to `server.Run`, and bound `pool.Close()` by `--shutdown-timeout` on `ErrUncleanShutdown` with `closeWithin` (design D3). Add a unit test for `closeWithin` and the test "Insert blocked in the database" in `cmd/rockets/server_test.go`. Verify with `make test-integration` (or the `cmd/rockets` tests against the test database) and the existing shutdown tests.
- [x] 1.4 Run `make lint`, `make test`, `make test-integration` and record any remaining limitations.
