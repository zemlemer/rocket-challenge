//go:build e2e

// Package e2e tests the running docker compose stack (nginx, server replicas,
// PostgreSQL) from the outside, the way the simulator uses it.
//
// Run with `make test-e2e`. The tests change the stack: they write to the
// inbox, stop and restart server containers, and the simulator test empties
// the inbox first.
package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"
)

var (
	baseURL = env("E2E_BASE_URL", "http://localhost:8088")
	dbURL   = env("E2E_DATABASE_URL", "postgres://rockets:rockets@localhost:15432/rockets?sslmode=disable")
	root    = repoRoot()
)

// TestMain waits until the entry point answers and every server replica is
// listening. Waiting for /healthz alone is not enough: it succeeds as soon as
// one replica is up, while the others may still be starting after a rebuild.
func TestMain(m *testing.M) {
	deadline := time.Now().Add(60 * time.Second)
	for !entryPointHealthy() || !allReplicasListening() {
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "e2e: stack not ready at %s (start it with `docker compose up --build -d`)\n", baseURL)
			os.Exit(1)
		}
		time.Sleep(500 * time.Millisecond)
	}
	os.Exit(m.Run())
}
