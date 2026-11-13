package main_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"rockets/internal/domain"
	"rockets/internal/processor"
	"rockets/internal/testdb"
)

func TestProcessorRefusesUnmigratedDatabase(t *testing.T) {
	r := rockets(t, []string{"DATABASE_URL=" + testdb.New(t)}, "processor")
	if r.code != 1 || !strings.Contains(r.stderr, "rockets migrate") {
		t.Fatalf("exit status = %d, want 1 naming rockets migrate:\n%s", r.code, r.stderr)
	}
}

func insertStream(t *testing.T, url string, rockets, perRocket int) {
	t.Helper()
	p := testdb.Pool(t, url)
	for r := range rockets {
		ch := fmt.Sprintf("rocket-%d", r)
		testdb.InsertEntry(t, p, ch, 1, domain.TypeLaunched, `{"type":"A","launchSpeed":100,"mission":"M"}`)
		for n := int64(2); n <= int64(perRocket); n++ {
			testdb.InsertEntry(t, p, ch, n, domain.TypeSpeedIncreased, `{"by":1}`)
		}
	}
}

func pending(t *testing.T, url string) int {
	t.Helper()
	var n int
	if err := testdb.Pool(t, url).QueryRow(context.Background(), `select count(*) from inbox where processed_at is null`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProcessorProcessesEntries(t *testing.T) {
	url := migrated(t)
	insertStream(t, url, 3, 50)
	startProcess(t, url, "processor started", "processor")
	deadline := time.Now().Add(10 * time.Second)
	for pending(t, url) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("entries not processed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := testdb.LoadRocket(t, testdb.Pool(t, url), "rocket-1").Speed; got != 149 {
		t.Fatalf("speed = %d, want 149", got)
	}
}

// SIGTERM while a large backlog is processed: exit 0, and the batch in
// progress is either committed or rolled back — a second processor run then
// reaches exactly the reference state (a half-applied batch would not).
func TestProcessorSigtermDuringProcessing(t *testing.T) {
	url := migrated(t)
	insertStream(t, url, 20, 1000)
	p := startProcess(t, url, "processor started", "processor")
	time.Sleep(150 * time.Millisecond)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("exit status %d after SIGTERM\n%s", exitErr.ExitCode(), p.stderr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("processor did not exit after SIGTERM")
	}
	if left := pending(t, url); left == 0 {
		t.Log("backlog was fully processed before the signal; partial-batch check is weaker")
	}

	startProcess(t, url, "processor started", "processor")
	deadline := time.Now().Add(30 * time.Second)
	for pending(t, url) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("backlog not drained")
		}
		time.Sleep(50 * time.Millisecond)
	}
	pool := testdb.Pool(t, url)
	for ch, want := range testdb.Expected(testdb.AllEntries(t, pool)) {
		if got := testdb.LoadRocket(t, pool, ch); testdb.Describe(got) != testdb.Describe(want) {
			t.Fatalf("%s:\n got  %s\n want %s", ch, testdb.Describe(got), testdb.Describe(want))
		}
	}
}

// Spec processor-runtime "Network hangs during rollback": the test's
// uncommitted rocket row makes the batch fail with lock_timeout, and the proxy
// freezes all traffic as soon as the processor sends its ROLLBACK, until the
// process has exited. The rollback's own timeout lets the loop return; the
// pool close then blocks (pgx closes the dead connection in the background
// for up to 15 s), so the process exits with status 1 at the budget.
func TestProcessorNetworkHangsDuringRollback(t *testing.T) {
	url := migrated(t)
	insertStream(t, url, 1, 50)
	total := pending(t, url)

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
	// The processor's upsert of rocket-0 waits on the primary key until
	// lock_timeout (5 s) ends it with an error and the batch rolls back.
	if _, err := tx.Exec(ctx, `insert into rockets (channel) values ('rocket-0')`); err != nil {
		t.Fatal(err)
	}

	proxy, proxied := testdb.NewProxy(t, url)
	frozen := proxy.FreezeOn("rollback")
	p := startProcess(t, proxied, "processor started", "processor")
	waitForLockWait(t, url)

	start := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-frozen:
	case <-time.After(10 * time.Second):
		t.Fatalf("processor sent no rollback\n%s", p.stderr)
	}
	select {
	case err := <-p.done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("exit = %v, want status 1\n%s", err, p.stderr)
		}
	case <-time.After(processor.ShutdownTimeout + 5*time.Second):
		t.Fatalf("processor did not exit while the network hung\n%s", p.stderr)
	}
	if elapsed := time.Since(start); elapsed > processor.ShutdownTimeout+time.Second {
		t.Errorf("exited %s after SIGTERM, want within the %s budget", elapsed, processor.ShutdownTimeout)
	}
	// The batch gave up its connection in time; only the pool close blocked.
	if out := p.stderr.String(); !strings.Contains(out, "shutdown was not clean: database pool still not closed") {
		t.Errorf("stderr lacks the pool-close message of an unclean shutdown:\n%s", out)
	}

	// Restore: closing the proxied connections ends the processor's server
	// session, which rolls its transaction back; then release the lock.
	proxy.Stop()
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if left := pending(t, url); left != total {
		t.Fatalf("%d of %d entries pending after the hung batch, want all", left, total)
	}

	startProcess(t, url, "processor started", "processor")
	deadline := time.Now().Add(30 * time.Second)
	for pending(t, url) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("entries not processed after recovery")
		}
		time.Sleep(50 * time.Millisecond)
	}
	pool := testdb.Pool(t, url)
	for ch, want := range testdb.Expected(testdb.AllEntries(t, pool)) {
		if got := testdb.LoadRocket(t, pool, ch); testdb.Describe(got) != testdb.Describe(want) {
			t.Fatalf("%s:\n got  %s\n want %s", ch, testdb.Describe(got), testdb.Describe(want))
		}
	}
}
