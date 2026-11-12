package postgres_test

import (
	"context"
	"testing"
	"testing/fstest"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

// Migrating from 0001 to 0002 keeps existing inbox entries, as unprocessed.
func TestProcessingMigrationKeepsInboxEntries(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t, testdb.New(t))
	only1 := fstest.MapFS{}
	all := postgres.Migrations()
	b, err := fsReadFile(all, "0001_inbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	only1["0001_inbox.sql"] = file(string(b))
	if _, err := postgres.Migrate(ctx, pool, only1, testdb.Logger()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into inbox (channel, message_number, body, truncated) values ('c', 1, '{}', false)`); err != nil {
		t.Fatal(err)
	}

	res, err := postgres.Migrate(ctx, pool, all, testdb.Logger())
	if err != nil || len(res.Applied) != 1 || res.Version != 2 {
		t.Fatalf("migrate to 2: res=%+v err=%v", res, err)
	}
	var pending int
	if err := pool.QueryRow(ctx, `select count(*) from inbox where processed_at is null and outcome is null`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("existing entry not kept as unprocessed: n=%d err=%v", pending, err)
	}
	if again, err := postgres.Migrate(ctx, pool, all, testdb.Logger()); err != nil || len(again.Applied) != 0 {
		t.Fatalf("re-run: %+v %v", again, err)
	}
}

func TestProcessingColumnsChecks(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	if _, err := pool.Exec(ctx, `insert into inbox (body, truncated) values ('x', false)`); err != nil {
		t.Fatal(err)
	}
	for name, sql := range map[string]string{
		"unknown outcome":      `update inbox set processed_at = now(), outcome = 'done'`,
		"outcome without time": `update inbox set outcome = 'applied'`,
		"time without outcome": `update inbox set processed_at = now()`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, sql); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, outcome := range []string{"applied", "quarantined", "failed"} {
		if _, err := pool.Exec(ctx, `update inbox set processed_at = now(), outcome = $1`, outcome); err != nil {
			t.Fatalf("%s rejected: %v", outcome, err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into rockets (channel) values ('c')`); err != nil {
		t.Fatalf("rockets defaults: %v", err)
	}
}
