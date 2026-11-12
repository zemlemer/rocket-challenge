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

	"rockets/internal/domain"
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
