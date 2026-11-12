package postgres_test

import (
	"context"
	"testing"
	"time"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

func TestSessionLimitsAreApplied(t *testing.T) {
	cfg, err := postgres.PoolConfig(testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	postgres.WithSessionLimits(cfg, postgres.ProcessorLimits)
	pool, err := postgres.ConnectConfig(context.Background(), cfg, 10*time.Second, testdb.Logger())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for setting, want := range map[string]string{
		"idle_in_transaction_session_timeout": "30s",
		"statement_timeout":                   "30s",
		"lock_timeout":                        "5s",
	} {
		var got string
		if err := pool.QueryRow(context.Background(), "show "+setting).Scan(&got); err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", setting, got, err, want)
		}
	}
}
