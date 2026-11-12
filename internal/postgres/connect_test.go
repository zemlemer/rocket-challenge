package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

func TestConnectGivesUpAfterTimeout(t *testing.T) {
	start := time.Now()
	_, err := postgres.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/db", time.Second, testdb.Logger())
	if err == nil || !strings.Contains(err.Error(), "could not be reached within 1s") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("gave up after %s, want about 1s", elapsed)
	}
}

func TestConnectInvalidURL(t *testing.T) {
	_, err := postgres.Connect(context.Background(), "postgres://u:p@host:notaport/db", time.Second, testdb.Logger())
	if err == nil || !strings.Contains(err.Error(), "invalid database URL") {
		t.Fatalf("err = %v", err)
	}
}

// The database becomes reachable only after Connect has started retrying:
// a TCP proxy to the real database starts listening 700ms later.
func TestConnectWaitsForLateDatabase(t *testing.T) {
	proxy, proxied := testdb.NewProxy(t, testdb.New(t))
	proxy.Stop() // nothing listens until the proxy starts again
	go func() {
		time.Sleep(700 * time.Millisecond)
		proxy.Start()
	}()

	pool, err := postgres.Connect(context.Background(), proxied, 10*time.Second, testdb.Logger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	pool.Close()
}
