package postgres_test

import (
	"context"
	"io/fs"
	"rockets/internal/postgres"
	"rockets/internal/testdb"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

func migrate(t *testing.T, pool *pgxpool.Pool, fsys fstest.MapFS) postgres.MigrateResult {
	t.Helper()
	res, err := postgres.Migrate(context.Background(), pool, fsys, testdb.Logger())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return res
}

func recorded(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(), "select version from schema_migrations order by version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var vs []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		vs = append(vs, v)
	}
	return vs
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "select to_regclass($1) is not null", name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func ids(ms []postgres.Migration) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID())
	}
	return out
}

// migratedPool returns a pool to a fresh database with the production schema.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, p := testdb.Migrated(t)
	return p
}

func fsReadFile(fsys fs.FS, name string) ([]byte, error) { return fs.ReadFile(fsys, name) }
