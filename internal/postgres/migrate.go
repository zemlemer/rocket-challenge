package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration is one forward-only schema step, read from a file named
// NNNN_name.sql (NNNN = version).
type Migration struct {
	Version int64
	Name    string
	SQL     string
}

// ID identifies the migration in logs and errors, e.g. "0001_inbox".
func (m Migration) ID() string { return fmt.Sprintf("%04d_%s", m.Version, m.Name) }

// MigrateResult reports what a migration run did.
type MigrateResult struct {
	Applied []Migration // applied by this run, in order
	Version int64       // highest applied version after the run (0 = none)
}

var migrationFile = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

// migrationLockKey is the session advisory lock taken for a whole run, so
// concurrent runs apply each migration once.
var migrationLockKey = func() int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("rockets.schema_migrations"))
	return int64(h.Sum64())
}()

// LoadMigrations reads *.sql files from the root of fsys, ordered by version.
// Other files (e.g. README.md) are ignored; a .sql file with a malformed name
// or a duplicate version is an error.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var ms []Migration
	seen := map[int64]string{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		parts := migrationFile.FindStringSubmatch(e.Name())
		if parts == nil {
			return nil, fmt.Errorf("migration file %q: name must match NNNN_name.sql", e.Name())
		}
		version, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration file %q: version must be a positive integer", e.Name())
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migration files %q and %q share version %d", other, e.Name(), version)
		}
		seen[version] = e.Name()
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		ms = append(ms, Migration{Version: version, Name: parts[2], SQL: string(body)})
	}
	slices.SortFunc(ms, func(a, b Migration) int { return int(a.Version - b.Version) })
	return ms, nil
}

// Migrate applies every migration from fsys that the database has not
// recorded yet, in version order. Each migration runs in its own transaction
// together with its schema_migrations record. The whole run holds a session
// advisory lock, so concurrent runs wait and then find nothing to do.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, log *slog.Logger) (MigrateResult, error) {
	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return MigrateResult{}, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", migrationLockKey); err != nil {
		return MigrateResult{}, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Use a fresh context: the run's context may already be cancelled.
		if _, err := conn.Exec(context.Background(), "select pg_advisory_unlock($1)", migrationLockKey); err != nil {
			// Closing the connection releases the session lock.
			_ = conn.Conn().Close(context.Background())
		}
	}()

	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (
		version    bigint      primary key,
		name       text        not null,
		applied_at timestamptz not null default now()
	)`); err != nil {
		return MigrateResult{}, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return MigrateResult{}, err
	}

	var res MigrateResult
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if err := applyOne(ctx, conn.Conn(), m); err != nil {
			return res, err
		}
		applied[m.Version] = true
		res.Applied = append(res.Applied, m)
		log.Info("applied migration", "version", m.Version, "name", m.Name)
	}

	for v := range applied {
		res.Version = max(res.Version, v)
	}
	if latest := latestVersion(migrations); res.Version > latest {
		log.Warn("database schema is newer than this executable", "database_version", res.Version, "executable_version", latest)
	}
	return res, nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) (map[int64]bool, error) {
	rows, err := conn.Query(ctx, "select version from schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	applied := make(map[int64]bool, len(versions))
	for _, v := range versions {
		applied[v] = true
	}
	return applied, nil
}

func applyOne(ctx context.Context, conn *pgx.Conn, m Migration) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// No arguments: pgx uses the simple protocol, so a file may hold several statements.
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "insert into schema_migrations (version, name) values ($1, $2)", m.Version, m.Name)
		return err
	})
	if err != nil {
		return fmt.Errorf("migration %s failed: %w", m.ID(), err)
	}
	return nil
}
