package postgres_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/postgres"
)

func TestPoolConfigDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		url      string
		maxConns int32
		connect  time.Duration
	}{
		{"defaults", "postgres://u:p@db:5432/x", postgres.DefaultMaxConns, postgres.DefaultConnectTimeout},
		{"URL overrides", "postgres://u:p@db:5432/x?pool_max_conns=3&connect_timeout=2", 3, 2 * time.Second},
		{"keyword form", "host=db user=u dbname=x pool_max_conns=4 connect_timeout=1", 4, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := postgres.PoolConfig(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxConns != tc.maxConns || cfg.ConnConfig.ConnectTimeout != tc.connect {
				t.Fatalf("MaxConns=%d ConnectTimeout=%s, want %d and %s",
					cfg.MaxConns, cfg.ConnConfig.ConnectTimeout, tc.maxConns, tc.connect)
			}
		})
	}
}

// A host that accepts TCP but never answers must not block a caller longer
// than the connect timeout, even with a much longer context deadline.
func TestConnectTimeoutBoundsUnresponsiveHost(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() }) // hold the connection, say nothing
		}
	}()

	cfg, err := postgres.PoolConfig("postgres://u:p@" + l.Addr().String() + "/x?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	if err := pool.Ping(ctx); err == nil {
		t.Fatal("ping to a silent host succeeded")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ping gave up after %s, want about the 1 s connect timeout", elapsed)
	}
}
