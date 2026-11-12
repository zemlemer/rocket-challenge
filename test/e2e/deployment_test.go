//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"rockets/internal/testdb"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Spec message-intake "Message stored", through the load balancer.
func TestPostThroughEntryPointIsStored(t *testing.T) {
	ch := uniqueChannel(t)
	if code := post(t, message(ch, 1)); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if n := queryInt(t, `select count(*) from inbox where channel = $1 and message_number = 1`, ch); n != 1 {
		t.Fatalf("inbox holds %d entries for the message, want 1", n)
	}
}

// Spec deployment "No size limit at the entry point".
func TestLargeBodyIsNotRejectedByEntryPoint(t *testing.T) {
	marker := uniqueChannel(t)
	body := marker + strings.Repeat("x", 2<<20)
	if code := post(t, body); code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (not 413)", code)
	}
	n := queryInt(t, `select count(*) from inbox
		where truncated and length(body) = 1048576 and position($1::bytea in body) = 1`, []byte(marker))
	if n != 1 {
		t.Fatalf("truncated 1 MiB entry not found (found %d)", n)
	}
}

// Spec deployment "Requests spread over instances".
func TestRequestsAreSpreadOverAllReplicas(t *testing.T) {
	replicas := len(containers(t, "server"))
	if replicas < 3 {
		t.Fatalf("%d server replicas running, want 3", replicas)
	}
	since := time.Now().Add(-time.Second)
	marker := "spread=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for range 30 {
		resp, err := http.Get(baseURL + "/healthz?" + marker)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	time.Sleep(time.Second) // let nginx flush its access log
	seen := map[string]int{}
	for _, u := range nginxUpstreams(t, since, marker) {
		seen[u]++
	}
	if len(seen) != replicas {
		t.Fatalf("requests reached %d upstreams %v, want all %d", len(seen), seen, replicas)
	}
}

// Spec deployment "Instance stopped during delivery": senders see only 2xx and
// every message is stored once.
func TestReplicaStoppedDuringDelivery(t *testing.T) {
	ch := uniqueChannel(t)
	victim := containers(t, "server")[0]
	t.Cleanup(func() { run(t, "docker", "start", victim) })

	const total, workers = 300, 3
	codes := make(chan int, total)
	next := make(chan int, total)
	for i := 1; i <= total; i++ {
		next <- i
	}
	close(next)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				codes <- post(t, message(ch, i))
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	time.Sleep(500 * time.Millisecond)
	run(t, "docker", "stop", "-t", "2", victim)
	time.Sleep(2 * time.Second)
	run(t, "docker", "start", victim)
	wg.Wait()
	close(codes)

	for code := range codes {
		if code != http.StatusAccepted {
			t.Fatalf("a sender got %d while a replica was stopped", code)
		}
	}
	if n := queryInt(t, `select count(distinct message_number) from inbox where channel = $1`, ch); n != total {
		t.Fatalf("inbox holds %d distinct messages, want %d", n, total)
	}
}

// Spec deployment "Instance crash": a process that dies (not a `docker stop`,
// which restart policies treat as intentional) is restarted automatically and
// serves again. The process is killed from the Docker VM's PID namespace.
func TestCrashedReplicaIsRestarted(t *testing.T) {
	victim := containers(t, "server")[0]
	restarts := func() int {
		n, _ := strconv.Atoi(strings.TrimSpace(run(t, "docker", "inspect", "-f", "{{.RestartCount}}", victim)))
		return n
	}
	before := restarts()
	pid := strings.TrimSpace(run(t, "docker", "inspect", "-f", "{{.State.Pid}}", victim))
	run(t, "docker", "run", "--rm", "--privileged", "--pid=host", "alpine:3.20", "kill", "-9", pid)

	deadline := time.Now().Add(30 * time.Second)
	for restarts() == before {
		if time.Now().After(deadline) {
			t.Fatalf("%s was not restarted after its process was killed", victim)
		}
		time.Sleep(500 * time.Millisecond)
	}
	for {
		state := strings.TrimSpace(run(t, "docker", "inspect", "-f", "{{.State.Status}}", victim))
		if state == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s after restart", victim, state)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// simulation is what one default simulator run against the stack measured.
type simulation struct {
	sent    int
	elapsed time.Duration
	maxAge  time.Duration // oldest unprocessed entry seen during the run
	drained time.Duration // after the run, until nothing was unprocessed
}

// simulate empties inbox and rockets, runs the simulator with its default
// options while sampling the oldest unprocessed entry every 100 ms, calls
// during (if any) a few seconds into the run, and waits until everything is
// processed.
func simulate(t *testing.T, during func()) simulation {
	t.Helper()
	sim := os.Getenv("ROCKETS_SIMULATOR")
	if sim == "" {
		t.Skip("set ROCKETS_SIMULATOR to the simulator binary from the challenge ZIP")
	}
	if _, err := db(t).Exec(t.Context(), "truncate inbox, rockets"); err != nil {
		t.Fatal(err)
	}

	var res simulation
	stop := make(chan struct{})
	sampled := make(chan time.Duration)
	go func() {
		conn := db(t)
		var maxAge time.Duration
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				sampled <- maxAge
				return
			case <-tick.C:
				var ms float64
				_ = conn.QueryRow(context.Background(),
					`select coalesce(extract(epoch from now() - min(received_at)) * 1000, 0) from inbox where processed_at is null`).Scan(&ms)
				maxAge = max(maxAge, time.Duration(ms)*time.Millisecond)
			}
		}
	}()
	if during != nil {
		go func() { time.Sleep(5 * time.Second); during() }()
	}

	start := time.Now()
	out, err := exec.Command(sim, "launch", baseURL+"/messages").CombinedOutput()
	res.elapsed = time.Since(start)
	if err != nil {
		t.Fatalf("simulator: %v\n%s", err, tail(string(out), 20))
	}
	res.sent = strings.Count(string(out), "Message sent")

	end := time.Now()
	for queryInt(t, `select count(*) from inbox where processed_at is null`) > 0 {
		if time.Since(end) > 30*time.Second {
			t.Fatal("inbox not processed 30 s after the run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	res.drained = time.Since(end)
	close(stop)
	res.maxAge = <-sampled
	t.Logf("simulator: %d messages in %s (%.0f msg/s); oldest unprocessed entry %s; drained %s after the run",
		res.sent, res.elapsed.Round(time.Millisecond), float64(res.sent)/res.elapsed.Seconds(), res.maxAge, res.drained.Round(time.Millisecond))
	return res
}

// assertStateMatchesInbox recomputes every rocket from the inbox (every entry
// is kept) with the domain rules and compares it field by field.
func assertStateMatchesInbox(t *testing.T) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	want := testdb.Expected(testdb.AllEntries(t, pool))
	if len(want) == 0 {
		t.Fatal("no rockets expected")
	}
	for ch, w := range want {
		if got := testdb.LoadRocket(t, pool, ch); testdb.Describe(got) != testdb.Describe(w) {
			t.Fatalf("%s:\n got  %s\n want %s", ch, testdb.Describe(got), testdb.Describe(w))
		}
	}
	if applied := queryInt(t, `select count(*) from inbox where outcome = 'applied'`); applied != queryInt(t, `select count(*) from inbox`) {
		t.Fatalf("only %d entries applied", applied)
	}
}

// Spec deployment "Default simulator run" and message-processing
// "Throughput and freshness", plus a full correctness check of the state.
func TestSimulatorDefaultRun(t *testing.T) {
	since := time.Now().Add(-time.Second)
	res := simulate(t, nil)

	if res.maxAge > time.Second {
		t.Errorf("oldest unprocessed entry was %s old, want ≤ 1 s", res.maxAge)
	}
	if res.drained > 2*time.Second {
		t.Errorf("entries unprocessed for %s after the run, want ≤ 2 s", res.drained)
	}
	rate := float64(res.sent) / res.elapsed.Seconds()
	if minRate, _ := strconv.ParseFloat(os.Getenv("E2E_MIN_RATE"), 64); minRate > 0 && rate < minRate {
		t.Errorf("throughput %.0f msg/s is below E2E_MIN_RATE=%.0f; compare request_time in the nginx log", rate, minRate)
	}

	time.Sleep(time.Second)
	logs := compose(t, "logs", "--no-log-prefix", "--since", since.UTC().Format(time.RFC3339), "nginx")
	posts, non2xx := 0, 0
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, `"POST /messages`) {
			continue
		}
		posts++
		if !strings.Contains(line, `" 202 `) {
			non2xx++
		}
	}
	if non2xx > 0 {
		t.Fatalf("%d of %d simulator requests did not get 202", non2xx, posts)
	}

	rows := queryInt(t, `select count(*) from inbox`)
	distinct := queryInt(t, `select count(distinct (channel, message_number)) from inbox`)
	anonymous := queryInt(t, `select count(*) from inbox where channel is null`)
	if incomplete := queryInt(t, `select count(*) from inbox where message_time is null or message_type is null`); incomplete != 0 {
		t.Fatalf("%d entries lack messageTime or messageType", incomplete)
	}
	if rows != distinct || anonymous != 0 || distinct != res.sent {
		t.Fatalf("inbox: %d rows, %d distinct identities, %d without identity; simulator sent %d", rows, distinct, anonymous, res.sent)
	}
	assertStateMatchesInbox(t)
}

// Spec deployment "One processor stopped": the others keep processing and
// every message is applied exactly once.
func TestSimulatorWithProcessorStopped(t *testing.T) {
	victim := containers(t, "processor")[0]
	t.Cleanup(func() { run(t, "docker", "start", victim) })
	simulate(t, func() { run(t, "docker", "stop", victim) })
	assertStateMatchesInbox(t)
}
