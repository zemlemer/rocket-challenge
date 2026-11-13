//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// assertAPIMatchesState compares GET /rockets and GET /rockets/{channel} with
// the rockets table, which assertStateMatchesInbox has checked against an
// independent recomputation from the inbox. Visibility, status and ordering
// are derived here in SQL, not by the server's code.
func assertAPIMatchesState(t *testing.T) {
	t.Helper()
	rows, err := db(t).Query(t.Context(), `
		select channel, rocket_type,
		       case when explosion_reason is null then 'active' else 'exploded' end,
		       speed, launch_speed, mission, launched_at, explosion_reason,
		       last_applied_message_number, last_applied_message_at
		from rockets
		where (rocket_type is not null or explosion_reason is not null)
		  and channel ~ '^[A-Za-z0-9_-]{1,255}$'
		order by channel collate "C"`)
	if err != nil {
		t.Fatal(err)
	}
	var want []apiRocket
	for rows.Next() {
		var r apiRocket
		if err := rows.Scan(&r.Channel, &r.Type, &r.Status, &r.Speed, &r.LaunchSpeed, &r.Mission,
			&r.LaunchedAt, &r.ExplosionReason, &r.LastMessageNumber, &r.LastMessageAt); err != nil {
			t.Fatal(err)
		}
		r.LaunchedAt, r.LastMessageAt = utc(r.LaunchedAt), utc(r.LastMessageAt)
		want = append(want, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var list struct {
		Rockets []apiRocket `json:"rockets"`
	}
	if code := getJSON(t, "/rockets", &list); code != http.StatusOK {
		t.Fatalf("GET /rockets = %d", code)
	}
	unlaunched := 0
	for _, r := range list.Rockets {
		if r.Type == nil {
			unlaunched++
		}
	}
	t.Logf("GET /rockets lists %d rockets (%d exploded before launch)", len(list.Rockets), unlaunched)
	if !reflect.DeepEqual(list.Rockets, want) {
		t.Fatalf("GET /rockets differs from the table:\n api   %+v\n table %+v", list.Rockets, want)
	}
	for _, w := range want {
		var single apiRocket
		if code := getJSON(t, "/rockets/"+url.PathEscape(w.Channel), &single); code != http.StatusOK || !reflect.DeepEqual(single, w) {
			t.Errorf("GET /rockets/%s = %d %+v, want %+v", w.Channel, code, single, w)
		}
	}
}

// Spec rocket-query "Processors stopped": reads keep answering with the last
// processed state.
func TestReadsWhileProcessorsStopped(t *testing.T) {
	ch := uniqueChannel(t)
	if code := post(t, fmt.Sprintf(`{"metadata":{"channel":%q,"messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"A","launchSpeed":500,"mission":"M"}}`, ch)); code != http.StatusAccepted {
		t.Fatalf("POST launch = %d", code)
	}
	var before apiRocket
	deadline := time.Now().Add(10 * time.Second)
	for getJSON(t, "/rockets/"+ch, &before) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("launch not processed within 10 s")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var listBefore struct {
		Rockets []apiRocket `json:"rockets"`
	}
	if code := getJSON(t, "/rockets", &listBefore); code != http.StatusOK {
		t.Fatalf("GET /rockets = %d", code)
	}

	processors := containers(t, "processor")
	t.Cleanup(func() {
		for _, c := range processors {
			run(t, "docker", "start", c)
		}
	})
	for _, c := range processors {
		run(t, "docker", "stop", c)
	}

	var after apiRocket
	if code := getJSON(t, "/rockets/"+ch, &after); code != http.StatusOK || !reflect.DeepEqual(after, before) {
		t.Fatalf("GET /rockets/%s with processors stopped = %d %+v, want %+v", ch, code, after, before)
	}
	var listAfter struct {
		Rockets []apiRocket `json:"rockets"`
	}
	if code := getJSON(t, "/rockets", &listAfter); code != http.StatusOK || !reflect.DeepEqual(listAfter, listBefore) {
		t.Fatalf("GET /rockets with processors stopped = %d, %d rockets; want the %d listed before", code, len(listAfter.Rockets), len(listBefore.Rockets))
	}
}

// Spec deployment "Read failures are not retried": a read (/rockets, /stats)
// that gets 503 from a slow database is answered after one attempt, and
// intake works as soon as the database answers again.
func TestSlowReadIsNotRetried(t *testing.T) {
	loaded := stripComments(compose(t, "exec", "-T", "nginx", "nginx", "-T"))
	for _, loc := range []string{"location /rockets {", "location /stats {"} {
		reads := block(t, loaded, loc)
		if strings.Contains(reads, "http_503") || !strings.Contains(reads, "proxy_next_upstream error timeout http_502;") {
			t.Fatalf("%s must not retry 503:\n%s", loc, reads)
		}
	}

	pg := containers(t, "postgres")[0]
	paused := true
	run(t, "docker", "pause", pg)
	t.Cleanup(func() {
		if paused {
			run(t, "docker", "unpause", pg)
		}
	})

	for _, path := range []string{"/rockets", "/stats"} {
		start := time.Now()
		resp, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		elapsed := time.Since(start)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s with the database paused = %d, want 503", path, resp.StatusCode)
		}
		if elapsed > 9*time.Second { // one 5 s attempt, not three
			t.Errorf("GET %s took %s: the 503 was retried on other replicas", path, elapsed)
		}
	}
	run(t, "docker", "unpause", pg)
	paused = false

	channel := uniqueChannel(t)
	for n := 1; n <= 6; n++ { // round-robin reaches every replica twice
		if code := post(t, message(channel, n)); code != http.StatusAccepted {
			t.Fatalf("POST %d right after the slow read = %d, want 202", n, code)
		}
	}
}
