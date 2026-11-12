package postgres_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

func TestLatestVersionOfEmbeddedSet(t *testing.T) {
	v, err := postgres.LatestVersion(postgres.Migrations())
	if err != nil || v < 1 {
		t.Fatalf("LatestVersion = %d, %v; want ≥ 1", v, err)
	}
}

func TestRequireSchema(t *testing.T) {
	ctx := context.Background()
	latest, _ := postgres.LatestVersion(postgres.Migrations())

	t.Run("unmigrated", func(t *testing.T) {
		pool := testdb.Pool(t, testdb.New(t))
		err := postgres.RequireSchema(ctx, pool, latest)
		if err == nil || !strings.Contains(err.Error(), "rockets migrate") {
			t.Fatalf("err = %v, want one naming rockets migrate", err)
		}
	})

	t.Run("latest", func(t *testing.T) {
		if err := postgres.RequireSchema(ctx, migratedPool(t), latest); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("newer database", func(t *testing.T) {
		pool := migratedPool(t)
		newer := fstest.MapFS{"9999_future.sql": file("create table future (id int);")}
		if _, err := postgres.Migrate(ctx, pool, newer, testdb.Logger()); err != nil {
			t.Fatal(err)
		}
		if err := postgres.RequireSchema(ctx, pool, latest); err != nil {
			t.Fatalf("a newer schema must be accepted: %v", err)
		}
	})

	t.Run("older database", func(t *testing.T) {
		pool := migratedPool(t)
		err := postgres.RequireSchema(ctx, pool, latest+1)
		if err == nil || !strings.Contains(err.Error(), "rockets migrate") {
			t.Fatalf("err = %v", err)
		}
	})
}
