package testdb_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"rockets/internal/testdb"
)

func TestNewCreatesIsolatedDatabases(t *testing.T) {
	a := testdb.New(t)
	b := testdb.New(t)
	if a == b {
		t.Fatalf("expected distinct databases, got %s twice", a)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var tables int
	if err := conn.QueryRow(ctx,
		`select count(*) from information_schema.tables where table_schema = 'public'`).Scan(&tables); err != nil {
		t.Fatalf("query: %v", err)
	}
	if tables != 0 {
		t.Fatalf("expected an empty database, found %d tables", tables)
	}
}
