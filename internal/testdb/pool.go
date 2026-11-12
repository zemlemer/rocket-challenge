package testdb

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/postgres"
)

// Logger returns a logger that discards everything.
func Logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Pool connects to url and closes the pool when the test ends.
func Pool(t testing.TB, url string) *pgxpool.Pool {
	t.Helper()
	p, err := postgres.Connect(context.Background(), url, 10*time.Second, Logger())
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// Migrated creates a fresh database with the production schema and returns
// its URL and a pool connected to it.
func Migrated(t testing.TB) (string, *pgxpool.Pool) {
	t.Helper()
	url := New(t)
	p := Pool(t, url)
	if _, err := postgres.Migrate(context.Background(), p, postgres.Migrations(), Logger()); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return url, p
}
