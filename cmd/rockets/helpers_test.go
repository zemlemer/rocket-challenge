package main_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"rockets/internal/testdb"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type result struct {
	code           int
	stdout, stderr string
}

// rockets runs the binary with a clean configuration environment plus env.
func rockets(t *testing.T, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	for _, kv := range os.Environ() {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "DATABASE_URL", "DB_CONNECT_TIMEOUT", "LOG_LEVEL":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run rockets: %v", err)
	}
	return result{code, stdout.String(), stderr.String()}
}

// migrated returns a fresh database URL after running `rockets migrate`.
func migrated(t *testing.T) string {
	t.Helper()
	url := testdb.New(t)
	if r := rockets(t, []string{"DATABASE_URL=" + url}, "migrate"); r.code != 0 {
		t.Fatalf("migrate: %s", r.stderr)
	}
	return url
}

type serverProc struct {
	cmd    *exec.Cmd
	addr   string
	stderr *strings.Builder
	done   chan error
}

// startServer runs `rockets server` on a free port and waits until it listens.
func startServer(t *testing.T, url string) *serverProc {
	t.Helper()
	return startServerArgs(t, url)
}

func startServerArgs(t *testing.T, url string, extra ...string) *serverProc {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	p := startProcess(t, url, "listening", append([]string{"server", "--http-addr", addr}, extra...)...)
	p.addr = addr
	return p
}

// startProcess runs the binary with args and waits until its log contains ready.
func startProcess(t *testing.T, url, ready string, args ...string) *serverProc {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(cmd.Environ(), "DATABASE_URL="+url, "LOG_LEVEL=debug")
	pipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &serverProc{cmd: cmd, stderr: &strings.Builder{}, done: make(chan error, 1)}
	started := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pipe)
		closed := false
		for sc.Scan() {
			p.stderr.WriteString(sc.Text() + "\n")
			if !closed && strings.Contains(sc.Text(), ready) {
				close(started)
				closed = true
			}
		}
		p.done <- cmd.Wait()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	select {
	case <-started:
	case err := <-p.done:
		t.Fatalf("%s exited early: %v\n%s", args[0], err, p.stderr)
	case <-time.After(15 * time.Second):
		t.Fatalf("%s did not start:\n%s", args[0], p.stderr)
	}
	return p
}

func inboxCount(t *testing.T, url string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var n int
	if err := conn.QueryRow(ctx, "select count(*) from inbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitForLockWait waits until a backend other than the caller's waits on a
// lock, i.e. the server's insert is blocked.
func waitForLockWait(t *testing.T, url string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from pg_stat_activity
			where datname = current_database() and wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no insert waits on the lock")
}

const testMessage = `{"metadata":{"channel":"c1","messageNumber":1,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`

// rawRequest opens a connection and sends the request head plus part of a body.
func rawRequest(t *testing.T, addr string, contentLength int, partial string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "POST /messages HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		contentLength, partial)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}
