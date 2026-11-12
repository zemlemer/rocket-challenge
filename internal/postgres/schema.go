package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LatestVersion returns the highest migration version in fsys (0 if none).
func LatestVersion(fsys fs.FS) (int64, error) {
	ms, err := LoadMigrations(fsys)
	if err != nil {
		return 0, err
	}
	return latestVersion(ms), nil
}

// SchemaVersion returns the highest applied migration version (0 when the
// database has never been migrated).
func SchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var v int64
	err := pool.QueryRow(ctx, "select coalesce(max(version), 0) from schema_migrations").Scan(&v)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}

// RequireSchema fails unless the database schema is at least at version min.
// Long-running modes call it at startup instead of migrating themselves.
func RequireSchema(ctx context.Context, pool *pgxpool.Pool, min int64) error {
	v, err := SchemaVersion(ctx, pool)
	if err != nil {
		return err
	}
	if v < min {
		return fmt.Errorf("database schema is at version %d, this executable needs %d: run `rockets migrate` first", v, min)
	}
	return nil
}
