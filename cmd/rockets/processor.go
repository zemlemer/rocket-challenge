package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"rockets/data"
	"rockets/internal/postgres"
	"rockets/internal/processor"
	"rockets/internal/repository"
)

// ProcessorCmd works through the inbox and maintains rocket state. Its
// settings are fixed in internal/processor; it serves no HTTP.
type ProcessorCmd struct{}

func (ProcessorCmd) needsDatabase() {}

// Run connects with session limits, checks the schema and processes until
// ctx is cancelled; then it lets the running batch finish within
// processor.ShutdownTimeout.
func (c *ProcessorCmd) Run(ctx context.Context, g *Globals, log *slog.Logger) error {
	host, _ := os.Hostname()
	log = log.With("instance", host)

	cfg, err := postgres.PoolConfig(g.DatabaseURL)
	if err != nil {
		return err
	}
	postgres.WithSessionLimits(cfg, postgres.ProcessorLimits)
	pool, err := postgres.ConnectConfig(ctx, cfg, g.DBConnectTimeout, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	required, err := postgres.LatestVersion(data.Migrations())
	if err != nil {
		return err
	}
	if err := postgres.RequireSchema(ctx, pool, required); err != nil {
		return err
	}

	p := processor.New(repository.NewProcessing(pool, log), log)
	errc := make(chan error, 1)
	go func() { errc <- p.Run(ctx) }()
	log.Info("processor started", "batch_size", processor.BatchSize, "poll_interval", processor.PollInterval)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", processor.ShutdownTimeout)
	select {
	case err := <-errc:
		return err
	case <-time.After(processor.ShutdownTimeout):
		return fmt.Errorf("shutdown was not clean: batch still running after %s", processor.ShutdownTimeout)
	}
}
