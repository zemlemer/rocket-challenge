package main_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"rockets/internal/testdb"
)

func TestMigrateModeOnEmptyDatabase(t *testing.T) {
	url := testdb.New(t)
	for run := 1; run <= 2; run++ {
		r := rockets(t, []string{"DATABASE_URL=" + url}, "migrate")
		if r.code != 0 {
			t.Fatalf("run %d: exit status = %d; stderr:\n%s", run, r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "schema is up to date") || !strings.Contains(r.stderr, "version=0") {
			t.Fatalf("run %d: expected 'up to date' at version 0:\n%s", run, r.stderr)
		}
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var exists bool
	if err := conn.QueryRow(ctx, "select to_regclass('schema_migrations') is not null").Scan(&exists); err != nil || !exists {
		t.Fatalf("schema_migrations missing: exists=%v err=%v", exists, err)
	}
}
