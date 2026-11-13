package main_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"rockets/internal/testdb"
)

func TestServerRefusesUnmigratedDatabase(t *testing.T) {
	r := rockets(t, []string{"DATABASE_URL=" + testdb.New(t)}, "server", "--http-addr", "127.0.0.1:0")
	if r.code != 1 || !strings.Contains(r.stderr, "rockets migrate") {
		t.Fatalf("exit status = %d, want 1 naming rockets migrate:\n%s", r.code, r.stderr)
	}
}

func TestServerStoresPostedMessage(t *testing.T) {
	url := migrated(t)
	srv := startServer(t, url)

	resp, err := http.Post("http://"+srv.addr+"/messages", "application/json", strings.NewReader(testMessage))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if n := inboxCount(t, url); n != 1 {
		t.Fatalf("inbox holds %d entries, want 1", n)
	}
}

func TestClientDisconnectMidBodyStoresNothing(t *testing.T) {
	url := migrated(t)
	srv := startServer(t, url)

	body := testMessage + strings.Repeat(" ", 10*1024-len(testMessage))
	conn := rawRequest(t, srv.addr, len(body), body[:2048])
	_ = conn.Close()

	time.Sleep(500 * time.Millisecond)
	if n := inboxCount(t, url); n != 0 {
		t.Fatalf("inbox holds %d entries after an aborted request, want 0", n)
	}

	resp, err := http.Post("http://"+srv.addr+"/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || inboxCount(t, url) != 1 {
		t.Fatalf("complete redelivery: status = %d, inbox = %d", resp.StatusCode, inboxCount(t, url))
	}
}

func TestSigtermLetsInFlightRequestComplete(t *testing.T) {
	url := migrated(t)
	srv := startServer(t, url)

	conn := rawRequest(t, srv.addr, len(testMessage), testMessage[:30])
	defer func() { _ = conn.Close() }()
	time.Sleep(200 * time.Millisecond) // request is in flight, body half sent

	if err := srv.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := io.WriteString(conn, testMessage[30:]); err != nil {
		t.Fatal(err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v\n%s", err, srv.stderr)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	select {
	case err := <-srv.done:
		if err != nil {
			t.Fatalf("server exit: %v\n%s", err, srv.stderr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not exit after SIGTERM")
	}
	if n := inboxCount(t, url); n != 1 {
		t.Fatalf("inbox holds %d entries, want 1", n)
	}
}

// A malformed chunked body is another way for a body not to be read
// completely: nothing is stored and a well-formed chunked redelivery is.
func TestMalformedChunkedBodyStoresNothing(t *testing.T) {
	url := migrated(t)
	srv := startServer(t, url)

	send := func(chunks string) *http.Response {
		t.Helper()
		conn, err := net.Dial("tcp", srv.addr)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		_, err = fmt.Fprintf(conn, "POST /messages HTTP/1.1\r\nHost: test\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n%s", chunks)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("read response: %v\n%s", err, srv.stderr)
		}
		_ = resp.Body.Close()
		return resp
	}
	half := len(testMessage) / 2

	bad := fmt.Sprintf("%x\r\n%s\r\nzz\r\n", half, testMessage[:half]) // invalid chunk size
	if resp := send(bad); resp.StatusCode < 500 {
		t.Fatalf("malformed chunked body: status = %d, want 5xx", resp.StatusCode)
	}
	if n := inboxCount(t, url); n != 0 {
		t.Fatalf("inbox holds %d entries after a malformed chunked body, want 0", n)
	}

	good := fmt.Sprintf("%x\r\n%s\r\n%x\r\n%s\r\n0\r\n\r\n", half, testMessage[:half], len(testMessage)-half, testMessage[half:])
	if resp := send(good); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("well-formed chunked body: status = %d, want 202", resp.StatusCode)
	}
	if n := inboxCount(t, url); n != 1 {
		t.Fatalf("inbox holds %d entries, want 1", n)
	}
}

// Spec http-server "Shutdown timeout exceeded": a request that never completes
// keeps the server from shutting down cleanly; it gives up after the timeout
// and exits with status 1.
func TestShutdownTimeoutExceededExitsWith1(t *testing.T) {
	url := migrated(t)
	srv := startServerArgs(t, url, "--shutdown-timeout", "1s")

	conn := rawRequest(t, srv.addr, len(testMessage), testMessage[:30]) // body never completed
	defer func() { _ = conn.Close() }()
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	if err := srv.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-srv.done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("exit = %v, want status 1\n%s", err, srv.stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit after the shutdown timeout")
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("exited after %s, before the 1 s shutdown timeout", elapsed)
	}
	if !strings.Contains(srv.stderr.String(), "shutdown was not clean") {
		t.Fatalf("stderr lacks the unclean-shutdown message:\n%s", srv.stderr)
	}
}

// Spec http-server "Insert blocked in the database": the test holds the lock
// on the message's identity until the server has exited, so only cancelling
// the insert and bounding the pool close let the process exit before the
// 10 s save timeout.
func TestShutdownCancelsInsertBlockedInDatabase(t *testing.T) {
	url := migrated(t)
	ctx := context.Background()
	lock, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close(ctx) }()
	tx, err := lock.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// An uncommitted entry with the same identity: the server's insert waits
	// on the unique index until this transaction ends.
	if _, err := tx.Exec(ctx, `insert into inbox (channel, message_number, body, truncated) values ('c1', 1, '{}', false)`); err != nil {
		t.Fatal(err)
	}

	srv := startServerArgs(t, url, "--shutdown-timeout", "1s")
	posted := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Post("http://"+srv.addr+"/messages", "application/json", strings.NewReader(testMessage))
		if err != nil {
			posted <- nil
			return
		}
		_ = resp.Body.Close()
		posted <- resp
	}()
	waitForLockWait(t, url)

	start := time.Now()
	if err := srv.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-srv.done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("exit = %v, want status 1\n%s", err, srv.stderr)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("server did not exit while its insert was blocked\n%s", srv.stderr)
	}
	elapsed := time.Since(start)
	if elapsed < time.Second || elapsed > 2*time.Second+500*time.Millisecond {
		t.Errorf("exited %s after SIGTERM, want between the 1 s timeout and twice it", elapsed)
	}

	// The lock is still held: the process exited without it being released.
	var holding bool
	if err := tx.QueryRow(ctx, `select count(*) = 1 from inbox where channel = 'c1'`).Scan(&holding); err != nil || !holding {
		t.Fatalf("lock transaction no longer usable after exit: %v", err)
	}
	if resp := <-posted; resp != nil && resp.StatusCode == http.StatusAccepted {
		t.Fatal("client got 202 for an insert that was cancelled")
	}
	out := srv.stderr.String()
	if !strings.Contains(out, "shutdown was not clean") {
		t.Errorf("stderr lacks the unclean-shutdown message:\n%s", out)
	}
	if strings.Contains(out, "pool still not closed") {
		t.Errorf("pool close hit its bound although the insert was cancelled:\n%s", out)
	}
}

// Spec http-server "Keep-alive": two requests on one connection.
func TestKeepAliveServesSeveralRequestsPerConnection(t *testing.T) {
	srv := startServer(t, migrated(t))
	conn, err := net.Dial("tcp", srv.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for i := 1; i <= 2; i++ {
		if _, err := fmt.Fprintf(conn, "GET /healthz HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
			t.Fatalf("request %d: write: %v", i, err)
		}
		resp, err := http.ReadResponse(r, nil)
		if err != nil {
			t.Fatalf("request %d on the same connection: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Close {
			t.Fatalf("request %d: status %d, close=%v", i, resp.StatusCode, resp.Close)
		}
	}
}

// Spec http-server "Slow headers": incomplete headers for more than 5 s close
// the connection.
func TestSlowHeadersCloseConnection(t *testing.T) {
	srv := startServer(t, migrated(t))
	conn, err := net.Dial("tcp", srv.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: te"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, err = io.ReadAll(conn) // returns when the server closes the connection
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("connection not closed by the server: %v", err)
	}
	if elapsed < 4*time.Second || elapsed > 8*time.Second {
		t.Fatalf("connection closed after %s, want about 5 s", elapsed)
	}
}
