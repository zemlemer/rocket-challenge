package main

import (
	"context"
	"log/slog"

	"rockets/internal/postgres"
)

// MigrateCmd brings the database schema to the latest embedded version.
type MigrateCmd struct{}

func (MigrateCmd) needsDatabase() {}

// Run connects to the database and applies all pending embedded migrations.
func (c *MigrateCmd) Run(ctx context.Context, g *Globals, log *slog.Logger) error {
	pool, err := postgres.Connect(ctx, g.DatabaseURL, g.DBConnectTimeout, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := postgres.Migrate(ctx, pool, postgres.Migrations(), log)
	if err != nil {
		return err
	}
	if len(res.Applied) == 0 {
		log.Info("schema is up to date", "version", res.Version)
	} else {
		log.Info("schema migrated", "version", res.Version, "applied", len(res.Applied))
	}
	return nil
}
