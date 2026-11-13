package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"rockets/data"
	"rockets/internal/postgres"
	"rockets/internal/processor"
	"rockets/internal/repository/processing"
)

// ProcessorCmd works through the inbox and maintains rocket state. Its
// settings are fixed in internal/processor; it serves no HTTP.
type ProcessorCmd struct{}

func (ProcessorCmd) needsDatabase() {}

// Run connects with session limits, checks the schema and processes until
// ctx is cancelled; then it finishes the running batch and closes the pool
// within processor.ShutdownTimeout.
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
	shuttingDown := false
	defer func() {
		if !shuttingDown { // no batch can hold a connection here
			pool.Close()
		}
	}()

	required, err := postgres.LatestVersion(data.Migrations())
	if err != nil {
		return err
	}
	if err := postgres.RequireSchema(ctx, pool, required); err != nil {
		return err
	}

	p := processor.New(processing.New(pool, log), log)
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	log.Info("processor started", "batch_size", processor.BatchSize, "poll_interval", processor.PollInterval)

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shuttingDown = true
	return shutdown(done, pool.Close, processor.ShutdownTimeout, log)
}

// shutdown waits for the processing loop to return and then closes the pool,
// both within one budget counted from now (design D2 of
// bound-processor-shutdown). Past the budget it returns an error and leaves
// whatever still blocks running in the background: the process exits anyway.
func shutdown(done <-chan error, closePool func(), budget time.Duration, log *slog.Logger) error {
	log.Info("shutting down", "timeout", budget)
	deadline := time.Now().Add(budget)
	t := time.NewTimer(budget)
	defer t.Stop()

	var runErr error
	select {
	case runErr = <-done:
	case <-t.C:
		return fmt.Errorf("shutdown was not clean: batch still running after %s", budget)
	}
	if !closeWithin(closePool, time.Until(deadline)) {
		return errors.Join(runErr, fmt.Errorf("shutdown was not clean: database pool still not closed after %s", budget))
	}
	return runErr
}
