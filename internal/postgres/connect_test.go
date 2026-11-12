package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"rockets/internal/postgres"
	"rockets/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
		800 * time.Millisecond, 1600 * time.Millisecond, 2 * time.Second, 2 * time.Second,
	}
	for attempt, w := range want {
		if got := postgres.Backoff(attempt); got != w {
			t.Errorf("Backoff(%d) = %s, want %s", attempt, got, w)
		}
	}
	if got := postgres.Backoff(1000); got != 2*time.Second {
		t.Errorf("Backoff(1000) = %s, want cap 2s", got)
	}
}

func TestConnectGivesUpAfterTimeout(t *testing.T) {
	start := time.Now()
	_, err := postgres.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/db", time.Second, quiet)
	if err == nil || !strings.Contains(err.Error(), "could not be reached within 1s") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("gave up after %s, want about 1s", elapsed)
	}
}

func TestConnectInvalidURL(t *testing.T) {
	_, err := postgres.Connect(context.Background(), "postgres://u:p@host:notaport/db", time.Second, quiet)
	if err == nil || !strings.Contains(err.Error(), "invalid database URL") {
		t.Fatalf("err = %v", err)
	}
}

// The database becomes reachable only after Connect has started retrying:
// a TCP proxy to the real database starts listening 700ms later.
func TestConnectWaitsForLateDatabase(t *testing.T) {
	dbURL := testdb.New(t)
	u, _ := url.Parse(dbURL)
	upstream := u.Host

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr := l.Addr().String()
	_ = l.Close() // nothing listens until the proxy starts

	go func() {
		time.Sleep(700 * time.Millisecond)
		startProxy(t, proxyAddr, upstream)
	}()

	u.Host = proxyAddr
	pool, err := postgres.Connect(context.Background(), u.String(), 10*time.Second, quiet)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	pool.Close()
}

func startProxy(t *testing.T, addr, upstream string) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Errorf("proxy listen: %v", err)
		return
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					return
				}
				defer func() { _ = up.Close() }()
				go func() { _, _ = io.Copy(up, c) }()
				_, _ = io.Copy(c, up)
			}()
		}
	}()
}
