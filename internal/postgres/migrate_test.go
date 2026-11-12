package postgres_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

func file(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }

var (
	m1 = file(`create table t1 (id int primary key);`)
	// m2 depends on m1, so applying out of order would fail.
	m2 = file(`create table t2 (id int primary key references t1);
	           insert into t1 values (100);`)
	broken = file(`create table t3 (id int);
	               this is not sql;`)
)

func TestMigrateEmptyDatabaseAppliesAllInOrder(t *testing.T) {
	pool := testdb.Pool(t, testdb.New(t))
	res := migrate(t, pool, fstest.MapFS{"0002_second.sql": m2, "0001_first.sql": m1, "README.md": file("docs")})

	if got := strings.Join(ids(res.Applied), ","); got != "0001_first,0002_second" {
		t.Fatalf("applied = %s", got)
	}
	if res.Version != 2 {
		t.Fatalf("version = %d, want 2", res.Version)
	}
	if got := recorded(t, pool); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("recorded = %v", got)
	}
}

func TestMigrateNoMigrationsCreatesBookkeepingOnly(t *testing.T) {
	pool := testdb.Pool(t, testdb.New(t))
	res := migrate(t, pool, fstest.MapFS{"README.md": file("docs")})
	if len(res.Applied) != 0 || res.Version != 0 {
		t.Fatalf("res = %+v", res)
	}
	if !tableExists(t, pool, "schema_migrations") {
		t.Fatal("schema_migrations not created")
	}
}

func TestMigrateAppliesOnlyNewMigrationAndKeepsData(t *testing.T) {
	pool := testdb.Pool(t, testdb.New(t))
	migrate(t, pool, fstest.MapFS{"0001_first.sql": m1})
	if _, err := pool.Exec(context.Background(), "insert into t1 values (1)"); err != nil {
		t.Fatal(err)
	}

	res := migrate(t, pool, fstest.MapFS{"0001_first.sql": m1, "0002_second.sql": m2})
	if got := strings.Join(ids(res.Applied), ","); got != "0002_second" {
		t.Fatalf("applied = %s", got)
	}
	var n int
	if err := pool.QueryRow(context.Background(), "select count(*) from t1 where id = 1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("existing row lost: n=%d err=%v", n, err)
	}
}

func TestMigrateSecondRunAppliesNothing(t *testing.T) {
	pool := testdb.Pool(t, testdb.New(t))
	fsys := fstest.MapFS{"0001_first.sql": m1, "0002_second.sql": m2}
	migrate(t, pool, fsys)
	res := migrate(t, pool, fsys)
	if len(res.Applied) != 0 || res.Version != 2 {
		t.Fatalf("second run: %+v", res)
	}
}

func TestMigrateBrokenMigrationIsAtomic(t *testing.T) {
	pool := testdb.Pool(t, testdb.New(t))
	_, err := postgres.Migrate(context.Background(), pool,
		fstest.MapFS{"0001_first.sql": m1, "0002_broken.sql": broken}, testdb.Logger())
	if err == nil || !strings.Contains(err.Error(), "0002_broken") {
		t.Fatalf("err = %v, want error naming 0002_broken", err)
	}
	if got := recorded(t, pool); len(got) != 1 || got[0] != 1 {
		t.Fatalf("recorded = %v, want [1]", got)
	}
	if tableExists(t, pool, "t3") {
		t.Fatal("partial schema change of the broken migration remained")
	}
	if !tableExists(t, pool, "t1") {
		t.Fatal("earlier migration was rolled back")
	}
}

func TestLoadMigrationsRejectsBadFiles(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"malformed name":    {"1-first.sql": m1},
		"version zero":      {"0000_zero.sql": m1},
		"duplicate version": {"0001_a.sql": m1, "01_b.sql": m2},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := postgres.LoadMigrations(fsys); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMigrateConcurrentRunsApplyOnce(t *testing.T) {
	url := testdb.New(t)
	fsys := fstest.MapFS{"0001_first.sql": m1, "0002_second.sql": m2}

	const runs = 3
	pools := make([]*pgxpool.Pool, runs)
	for i := range pools {
		pools[i] = testdb.Pool(t, url) // separate pools = separate sessions, like separate processes
	}

	var wg sync.WaitGroup
	errs := make(chan error, runs)
	applied := make(chan int, runs)
	start := make(chan struct{})
	for _, p := range pools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := postgres.Migrate(context.Background(), p, fsys, testdb.Logger())
			errs <- err
			applied <- len(res.Applied)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(applied)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent run failed: %v", err)
		}
	}
	total := 0
	for n := range applied {
		total += n
	}
	if total != 2 {
		t.Fatalf("migrations applied %d times in total, want 2", total)
	}
	if got := recorded(t, pools[0]); len(got) != 2 {
		t.Fatalf("recorded = %v", got)
	}
}
