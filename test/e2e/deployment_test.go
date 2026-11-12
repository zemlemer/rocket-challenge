//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

// assertStateMatchesInbox independently recomputes rocket state in SQL from
// applied inbox messages, without using the processor's domain implementation.
func assertStateMatchesInbox(t *testing.T) {
	t.Helper()
	if applied := queryInt(t, `select count(*) from inbox where outcome = 'applied'`); applied != queryInt(t, `select count(*) from inbox`) {
		t.Fatalf("only %d entries applied", applied)
	}
	if n := queryInt(t, `select count(*) from rockets`); n == 0 {
		t.Fatal("no rockets stored")
	}

	// Each applied body is conforming JSON. The query derives state from event
	// fields rather than calling Fold/Apply, and the full join catches missing
	// and unexpected rocket rows as well as incorrect column values.
	const compare = `
		with applied as (
			select channel, message_number, message_time, message_type,
			       convert_from(body, 'UTF8')::jsonb -> 'message' as payload
			from inbox where outcome = 'applied'
		), speed as (
			select channel, sum(case message_type
				when 'RocketLaunched' then (payload ->> 'launchSpeed')::bigint
				when 'RocketSpeedIncreased' then (payload ->> 'by')::bigint
				when 'RocketSpeedDecreased' then -(payload ->> 'by')::bigint
				else 0 end)::bigint as speed
			from applied group by channel
		), launch as (
			select channel, payload ->> 'type' as rocket_type,
			       (payload ->> 'launchSpeed')::bigint as launch_speed,
			       message_time as launched_at
			from applied where message_type = 'RocketLaunched'
		), mission as (
			select distinct on (channel) channel, message_number as mission_message_number,
			       case message_type when 'RocketLaunched' then payload ->> 'mission'
			                         else payload ->> 'newMission' end as mission
			from applied where message_type in ('RocketLaunched', 'RocketMissionChanged')
			order by channel, message_number desc
		), explosion as (
			select channel, payload ->> 'reason' as explosion_reason
			from applied where message_type = 'RocketExploded'
		), latest as (
			select distinct on (channel) channel, message_number as last_applied_message_number,
			       message_time as last_applied_message_at
			from applied order by channel, message_number desc
		), expected as (
			select s.channel, s.speed, l.rocket_type, l.launch_speed, l.launched_at,
			       m.mission, coalesce(m.mission_message_number, 0) as mission_message_number,
			       x.explosion_reason, z.last_applied_message_number, z.last_applied_message_at
			from speed s
			left join launch l using (channel)
			left join mission m using (channel)
			left join explosion x using (channel)
			join latest z using (channel)
		), actual as (
			select channel, speed, rocket_type, launch_speed, launched_at, mission,
			       mission_message_number, explosion_reason, last_applied_message_number,
			       last_applied_message_at from rockets
		)
		select coalesce(e.channel, a.channel),
		       coalesce(to_jsonb(e)::text, '<missing>'),
		       coalesce(to_jsonb(a)::text, '<missing>')
		from expected e full join actual a using (channel)
		where e.channel is null or a.channel is null or
		      row(e.speed, e.rocket_type, e.launch_speed, e.launched_at, e.mission,
		          e.mission_message_number, e.explosion_reason, e.last_applied_message_number,
		          e.last_applied_message_at)
		      is distinct from
		      row(a.speed, a.rocket_type, a.launch_speed, a.launched_at, a.mission,
		          a.mission_message_number, a.explosion_reason, a.last_applied_message_number,
		          a.last_applied_message_at)
		order by 1`
	rows, err := db(t).Query(t.Context(), compare)
	if err != nil {
		t.Fatalf("recompute state from inbox: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var channel string
		var expected, actual string
		if err := rows.Scan(&channel, &expected, &actual); err != nil {
			t.Fatal(err)
		}
		t.Errorf("rocket %s differs from inbox:\n expected %v\n actual   %v", channel, expected, actual)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
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
