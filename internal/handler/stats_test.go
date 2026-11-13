package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"rockets/internal/handler"
	"rockets/internal/testdb"
)

type fakeStats struct {
	snap handler.StatsSnapshot
	err  error
}

func (f *fakeStats) Stats(ctx context.Context) (handler.StatsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return handler.StatsSnapshot{}, err
	}
	return f.snap, f.err
}

var statsAsOf = time.Date(2026, 11, 13, 10, 0, 0, 123456000, time.UTC)

func getStats(t *testing.T, repo handler.StatsRepository) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	(&handler.Stats{Repo: repo, Log: testdb.Logger()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	return rec
}

type statsBody struct {
	AsOf    string
	Inbox   map[string]any
	Rockets []map[string]any
}

func decodeStats(t *testing.T, rec *httptest.ResponseRecorder) statsBody {
	t.Helper()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q, body %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	var b statsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || b.Rockets == nil {
		t.Fatalf("body %s: %v (rockets must be an array)", rec.Body, err)
	}
	return b
}

func TestStatsEmptyReport(t *testing.T) {
	b := decodeStats(t, getStats(t, &fakeStats{snap: handler.StatsSnapshot{AsOf: statsAsOf}}))
	want := map[string]any{"received": 0.0, "pending": 0.0, "applied": 0.0, "quarantined": 0.0, "failed": 0.0,
		"oldestPendingAt": nil, "oldestPendingAgeSeconds": 0.0}
	if !reflect.DeepEqual(b.Inbox, want) || b.AsOf != "2026-11-13T10:00:00.123456Z" || len(b.Rockets) != 0 {
		t.Fatalf("report = %+v", b)
	}
}

func TestStatsBacklog(t *testing.T) {
	oldest := statsAsOf.Add(-2500 * time.Millisecond)
	b := decodeStats(t, getStats(t, &fakeStats{snap: handler.StatsSnapshot{AsOf: statsAsOf,
		Inbox: handler.InboxTotals{Received: 11, Pending: 3, Applied: 5, Quarantined: 2, Failed: 1, OldestPendingAt: &oldest}}}))
	if b.Inbox["oldestPendingAgeSeconds"] != 2.5 || b.Inbox["oldestPendingAt"] != "2026-11-13T09:59:57.623456Z" ||
		b.Inbox["received"] != 11.0 || b.Inbox["pending"] != 3.0 {
		t.Fatalf("inbox = %v", b.Inbox)
	}
}

func TestStatsRocketCompleteness(t *testing.T) {
	snap := handler.StatsSnapshot{AsOf: statsAsOf, Channels: []handler.ChannelStats{
		{Channel: "pending", Highest: 4, Received: 4, Pending: 1, Applied: 3},
		{Channel: "gap", Highest: 5, Received: 3, Applied: 3},
		{Channel: "bad channel", Highest: 1, Received: 1, Applied: 1},
		{Channel: "Quarantined", Highest: 4, Received: 4, Applied: 3, Quarantined: 1},
		{Channel: "failed", Highest: 2, Received: 2, Applied: 1, Failed: 1},
	}}
	b := decodeStats(t, getStats(t, &fakeStats{snap: snap}))

	type row struct {
		channel  string
		missing  float64
		complete bool
	}
	want := []row{{"Quarantined", 0, true}, {"failed", 0, false}, {"gap", 2, false}, {"pending", 0, false}}
	var got []row
	for _, r := range b.Rockets {
		got = append(got, row{r["channel"].(string), r["missing"].(float64), r["complete"].(bool)})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rockets = %v, want %v", got, want)
	}
	fields := map[string]any{"channel": "Quarantined", "highestMessageNumber": 4.0, "received": 4.0, "missing": 0.0,
		"pending": 0.0, "applied": 3.0, "quarantined": 1.0, "failed": 0.0, "complete": true}
	if !reflect.DeepEqual(b.Rockets[0], fields) {
		t.Errorf("rocket = %v, want %v", b.Rockets[0], fields)
	}
}

func TestStatsReadFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"unavailable": {errors.Join(handler.ErrUnavailable, errors.New("conn refused")), http.StatusServiceUnavailable},
		"timeout":     {fmt.Errorf("read stats: %w", context.DeadlineExceeded), http.StatusServiceUnavailable},
		"other":       {errors.New("scan failed"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec := getStats(t, &fakeStats{err: tc.err})
			if rec.Code != tc.want || decode(t, rec)["error"] == nil {
				t.Fatalf("status %d body %s, want %d with error", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestStatsClientGoneIsNotAnError(t *testing.T) {
	var logs bytes.Buffer
	h := &handler.Stats{Repo: &fakeStats{}, Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil).WithContext(ctx))
	if rec.Body.Len() != 0 || logs.Len() != 0 {
		t.Fatalf("body %q, logs %q; want neither", rec.Body, logs.String())
	}
}
