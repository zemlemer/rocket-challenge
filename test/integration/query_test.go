package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rockets/internal/domain"
	"rockets/internal/repository/processing"
	"rockets/internal/testdb"
)

// Messages posted through intake and processed become readable through the
// real routes; a historical row with an invalid channel stays hidden.
func TestQueryAPIReadsProcessedState(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	log := testdb.Logger()
	routes := serverRoutes(p)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	msg := func(channel string, number int, typ, payload string) string {
		return fmt.Sprintf(`{"metadata":{"channel":%q,"messageNumber":%d,"messageTime":"2022-02-02T19:39:0%d.86337+01:00","messageType":%q},"message":%s}`,
			channel, number, number, typ, payload)
	}
	for _, body := range []string{
		msg("c1", 2, domain.TypeSpeedIncreased, `{"by":300}`),
		msg("c1", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
		msg("c2", 3, domain.TypeExploded, `{"reason":"FROZEN_O_RINGS"}`), // exploded before launch
		msg("c3", 2, domain.TypeSpeedIncreased, `{"by":1}`),              // neither: hidden
	} {
		if rec := do(http.MethodPost, "/messages", body); rec.Code != http.StatusAccepted {
			t.Fatalf("POST %s: %d", body, rec.Code)
		}
	}
	if rec := do(http.MethodGet, "/rockets/c1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("before processing: GET /rockets/c1 = %d, want 404", rec.Code)
	}
	if _, err := processing.New(p, log).ProcessBatch(ctx, 100); err != nil {
		t.Fatal(err)
	}
	// A launched row stored before the channel rule existed.
	if _, err := p.Exec(ctx, `insert into rockets (channel, speed, rocket_type, launch_speed, launched_at, mission)
		values ('a.b', 1, 'A', 1, now(), 'M')`); err != nil {
		t.Fatal(err)
	}

	rec := do(http.MethodGet, "/rockets/c1", "")
	var c1 map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &c1); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /rockets/c1 = %d %s", rec.Code, rec.Body)
	}
	if c1["type"] != "Falcon-9" || c1["speed"] != 800.0 || c1["status"] != "active" || c1["launchedAt"] != "2022-02-02T18:39:01.86337Z" || c1["lastMessageNumber"] != 2.0 {
		t.Fatalf("c1 = %v", c1)
	}

	rec = do(http.MethodGet, "/rockets?sort=type&order=desc", "")
	var list struct {
		Rockets []map[string]any `json:"rockets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /rockets = %d %s", rec.Code, rec.Body)
	}
	if len(list.Rockets) != 2 || list.Rockets[0]["channel"] != "c1" || list.Rockets[1]["channel"] != "c2" {
		t.Fatalf("list = %v, want c1 then c2 (null type last)", list.Rockets)
	}
	if c2 := list.Rockets[1]; c2["status"] != "exploded" || c2["type"] != nil || c2["speed"] != 0.0 {
		t.Fatalf("c2 = %v", c2)
	}
	for k, v := range c1 {
		if list.Rockets[0][k] != v {
			t.Errorf("list c1 %s = %v, single = %v", k, list.Rockets[0][k], v)
		}
	}

	for _, path := range []string{"/rockets/a.b", "/rockets/a%2Eb", "/rockets/c3", "/rockets/C1"} {
		if rec := do(http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	var stillThere int
	if err := p.QueryRow(ctx, `select count(*) from rockets where channel = 'a.b'`).Scan(&stillThere); err != nil || stillThere != 1 {
		t.Fatalf("historical row count = %d, %v; want it kept", stillThere, err)
	}
}

// Spec rocket-query "Read during processing": a read does not wait for a
// transaction that is changing rocket state and sees only committed state.
func TestReadDuringProcessing(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`)
	if _, err := processing.New(p, testdb.Logger()).ProcessBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	routes := serverRoutes(p)

	tx, err := p.Begin(ctx) // stands in for a processor batch that has not committed
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update rockets set speed = speed + 1000, mission = 'MARS' where channel = 'c1'`); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	single := getRocket(t, routes, "c1")
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rockets", nil))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("reads took %s while a transaction held the row", elapsed)
	}
	if single["speed"] != 500.0 || single["mission"] != "ARTEMIS" || !strings.Contains(rec.Body.String(), `"speed":500`) {
		t.Fatalf("read uncommitted state: single %v, list %s", single, rec.Body)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if after := getRocket(t, routes, "c1"); after["speed"] != 1500.0 || after["mission"] != "MARS" {
		t.Fatalf("after commit: %v", after)
	}
}

// Spec rocket-query "Mission known before launch", "Gap in applied messages"
// and "Times from the sender's clock", through processing.
func TestFieldsBeforeLaunch(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	testdb.InsertEntry(t, p, "c1", 3, domain.TypeMissionChanged, `{"newMission":"MARS"}`)
	testdb.InsertEntry(t, p, "c1", 4, domain.TypeExploded, `{"reason":"BOOM"}`)
	if _, err := p.Exec(ctx, `update inbox set message_time = $1 where message_number = 4`, testdb.MessageTime.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	proc := processing.New(p, testdb.Logger())
	if _, err := proc.ProcessBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	routes := serverRoutes(p)

	r := getRocket(t, routes, "c1")
	if r["mission"] != "MARS" || r["type"] != nil || r["launchSpeed"] != nil || r["launchedAt"] != nil || r["status"] != "exploded" || r["lastMessageNumber"] != 4.0 {
		t.Fatalf("before launch: %v", r)
	}

	testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`)
	if _, err := proc.ProcessBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	r = getRocket(t, routes, "c1")
	// Mission stays from the higher-numbered change; message 2 never arrived,
	// yet lastMessageNumber is 4; message 4's time is earlier than the launch's.
	wantLaunched := testdb.MessageTime.Add(time.Second).UTC().Format(time.RFC3339Nano)
	wantLast := testdb.MessageTime.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if r["mission"] != "MARS" || r["type"] != "Falcon-9" || r["speed"] != 500.0 || r["lastMessageNumber"] != 4.0 ||
		r["launchedAt"] != wantLaunched || r["lastMessageAt"] != wantLast {
		t.Fatalf("after launch: %v (want launchedAt %s, lastMessageAt %s)", r, wantLaunched, wantLast)
	}
}
