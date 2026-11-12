// Package testdb provides isolated PostgreSQL databases for integration tests.
//
// Integration tests call New, which skips the test unless TEST_DATABASE_URL is
// set. With the variable set, every call creates a fresh, empty database and
// drops it when the test finishes, so tests are independent and may run in
// parallel.
package testdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnvVar names the environment variable holding the admin connection URL.
const EnvVar = "TEST_DATABASE_URL"

// AdminURL returns TEST_DATABASE_URL or skips the test when it is unset.
func AdminURL(t testing.TB) string {
	t.Helper()
	u := os.Getenv(EnvVar)
	if u == "" {
		t.Skipf("integration test: set %s (see `make db-up` and `make test-integration`)", EnvVar)
	}
	return u
}

// New creates an empty database for the test and returns its connection URL.
// The database is dropped during test cleanup.
func New(t testing.TB) string {
	t.Helper()
	admin := AdminURL(t)

	name := "test_" + randomSuffix(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("testdb: connect to %s: %v", EnvVar, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("testdb: create database %s: %v", name, err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Errorf("testdb: connect for cleanup: %v", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop database %s: %v", name, err)
		}
	})

	return withDatabase(t, admin, name)
}
