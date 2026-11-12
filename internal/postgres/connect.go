package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"rockets/pkg/helpers"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool defaults, applied unless the database URL sets the parameter.
const (
	DefaultConnectTimeout = 5 * time.Second // URL parameter connect_timeout
	DefaultMaxConns       = 10              // URL parameter pool_max_conns
)

var (
	hasConnectTimeout = regexp.MustCompile(`(^|[?&\s])connect_timeout=`)
	hasPoolMaxConns   = regexp.MustCompile(`(^|[?&\s])pool_max_conns=`)
)

// PoolConfig parses databaseURL and applies the pool defaults: a bounded
// time to establish a connection and a bounded pool size. pgx's own defaults
// are no connect timeout and a size that depends on the CPU count.
func PoolConfig(databaseURL string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	if !hasConnectTimeout.MatchString(databaseURL) {
		cfg.ConnConfig.ConnectTimeout = DefaultConnectTimeout
	}
	if !hasPoolMaxConns.MatchString(databaseURL) {
		cfg.MaxConns = DefaultMaxConns
	}
	return cfg, nil
}

// Connect opens a connection pool and waits until the database answers,
// retrying with helpers.Backoff for up to timeout. The returned pool must be closed
// by the caller.
func Connect(ctx context.Context, databaseURL string, timeout time.Duration, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := PoolConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return ConnectConfig(ctx, cfg, timeout, log)
}

// ConnectConfig is Connect for an already prepared pool configuration.
func ConnectConfig(ctx context.Context, cfg *pgxpool.Config, timeout time.Duration, log *slog.Logger) (*pgxpool.Pool, error) {
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

		wait := helpers.Backoff(attempt)
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
