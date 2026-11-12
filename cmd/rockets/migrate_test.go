package main_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

func TestMigrateModeAppliesThenReportsUpToDate(t *testing.T) {
	url := testdb.New(t)
	first := rockets(t, []string{"DATABASE_URL=" + url}, "migrate")
	if first.code != 0 {
		t.Fatalf("first run: exit status = %d; stderr:\n%s", first.code, first.stderr)
	}
	latest, err := postgres.LatestVersion(postgres.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	version := fmt.Sprintf("version=%d", latest)
	for _, want := range []string{"applied migration", "name=inbox", "name=processing", "schema migrated", version} {
		if !strings.Contains(first.stderr, want) {
			t.Fatalf("first run: log lacks %q:\n%s", want, first.stderr)
		}
	}

	second := rockets(t, []string{"DATABASE_URL=" + url}, "migrate")
	if second.code != 0 || !strings.Contains(second.stderr, "schema is up to date") || !strings.Contains(second.stderr, version) {
		t.Fatalf("second run: exit status = %d, want 'up to date' at %s:\n%s", second.code, version, second.stderr)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var exists bool
	if err := conn.QueryRow(ctx, "select to_regclass('inbox') is not null").Scan(&exists); err != nil || !exists {
		t.Fatalf("inbox missing: exists=%v err=%v", exists, err)
	}
}
