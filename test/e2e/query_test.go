//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// apiRocket is the public representation read back from the API; times are
// decoded so that they can be compared with the database's.
type apiRocket struct {
	Channel           string     `json:"channel"`
	Type              *string    `json:"type"`
	Status            string     `json:"status"`
	Speed             int64      `json:"speed"`
	LaunchSpeed       *int64     `json:"launchSpeed"`
	Mission           *string    `json:"mission"`
	LaunchedAt        *time.Time `json:"launchedAt"`
	ExplosionReason   *string    `json:"explosionReason"`
	LastMessageNumber int64      `json:"lastMessageNumber"`
	LastMessageAt     *time.Time `json:"lastMessageAt"`
}

func getJSON(t *testing.T, path string, v any) int {
	t.Helper()
	resp, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
	}
	return resp.StatusCode
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

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

// Spec rocket-query "Processors stopped": reads keep working from stored state.
func TestReadsWhileProcessorsStopped(t *testing.T) {
	processors := containers(t, "processor")
	t.Cleanup(func() {
		for _, c := range processors {
			run(t, "docker", "start", c)
		}
	})
	for _, c := range processors {
		run(t, "docker", "stop", c)
	}
	var list struct {
		Rockets []apiRocket `json:"rockets"`
	}
	if code := getJSON(t, "/rockets", &list); code != http.StatusOK || list.Rockets == nil {
		t.Fatalf("GET /rockets with all processors stopped = %d", code)
	}
}

// Spec deployment "Read failures are not retried": a read that gets 503 from a
// slow database is answered after one attempt, and intake works as soon as
// the database answers again.
func TestSlowReadIsNotRetried(t *testing.T) {
	loaded := stripComments(compose(t, "exec", "-T", "nginx", "nginx", "-T"))
	reads := block(t, loaded, "location /rockets {")
	if strings.Contains(reads, "http_503") || !strings.Contains(reads, "proxy_next_upstream error timeout http_502;") {
		t.Fatalf("location /rockets must not retry 503:\n%s", reads)
	}

	pg := containers(t, "postgres")[0]
	paused := true
	run(t, "docker", "pause", pg)
	t.Cleanup(func() {
		if paused {
			run(t, "docker", "unpause", pg)
		}
	})

	start := time.Now()
	resp, err := http.Get(baseURL + "/rockets")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	elapsed := time.Since(start)
	run(t, "docker", "unpause", pg)
	paused = false

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /rockets with the database paused = %d, want 503", resp.StatusCode)
	}
	if elapsed > 9*time.Second { // one 5 s attempt, not three
		t.Errorf("GET /rockets took %s: the 503 was retried on other replicas", elapsed)
	}
	channel := uniqueChannel(t)
	for n := 1; n <= 6; n++ { // round-robin reaches every replica twice
		if code := post(t, message(channel, n)); code != http.StatusAccepted {
			t.Fatalf("POST %d right after the slow read = %d, want 202", n, code)
		}
	}
}
