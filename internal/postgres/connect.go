package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	backoffInitial = 100 * time.Millisecond
	backoffMax     = 2 * time.Second
	pingTimeout    = 2 * time.Second
)

// Backoff returns the delay before retry number attempt (0-based):
// 100ms doubling per attempt, capped at 2s.
func Backoff(attempt int) time.Duration {
	d := backoffInitial
	for i := 0; i < attempt && d < backoffMax; i++ {
		d *= 2
	}
	return min(d, backoffMax)
}

// Connect opens a connection pool and waits until the database answers,
// retrying with Backoff for up to timeout. The returned pool must be closed
// by the caller.
func Connect(ctx context.Context, databaseURL string, timeout time.Duration, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	target := fmt.Sprintf("%s:%d/%s", cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool for %s: %w", target, err)
	}

	deadline := time.Now().Add(timeout)
	for attempt := 0; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, min(pingTimeout, max(time.Until(deadline), time.Millisecond)))
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			log.Debug("connected to database", "target", target, "attempts", attempt+1)
			return pool, nil
		}

		wait := Backoff(attempt)
		if ctx.Err() != nil || time.Now().Add(wait).After(deadline) {
			pool.Close()
			return nil, fmt.Errorf("database %s could not be reached within %s: %w", target, timeout, err)
		}
		log.Info("database not reachable yet, retrying", "target", target, "retry_in", wait, "error", err)

		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database %s could not be reached: %w", target, ctx.Err())
		case <-time.After(wait):
		}
	}
}
