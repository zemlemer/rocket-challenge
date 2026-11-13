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
	"slices"
	"strings"
	"testing"
	"time"

	"rockets/internal/domain"
	"rockets/internal/handler"
)

var launchTime = time.Date(2022, 2, 2, 19, 39, 5, 863370000, time.FixedZone("", 3600))

func launched() domain.Rocket {
	return domain.Rocket{
		Channel: "c1", Speed: 800, RocketType: ptr("Falcon-9"), LaunchSpeed: ptr(int64(500)),
		LaunchedAt: ptr(launchTime), Mission: ptr("ARTEMIS"), MissionNumber: 1,
		LastAppliedNumber: 2, LastAppliedAt: ptr(launchTime.Add(time.Second)),
	}
}

func TestRocketRepresentation(t *testing.T) {
	rec := getRocket(t, &fakeRockets{rockets: []domain.Rocket{launched()}}, "c1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	got := decode(t, rec)
	want := map[string]any{
		"channel": "c1", "type": "Falcon-9", "status": "active", "speed": 800.0, "launchSpeed": 500.0,
		"mission": "ARTEMIS", "launchedAt": "2022-02-02T18:39:05.86337Z", "explosionReason": nil,
		"lastMessageNumber": 2.0, "lastMessageAt": "2022-02-02T18:39:06.86337Z",
	}
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v, want %#v", k, got[k], v)
		}
	}
}

func TestRocketExplodedBeforeLaunch(t *testing.T) {
	r := domain.Rocket{Channel: "c1", Speed: 300, ExplosionReason: ptr("FROZEN_O_RINGS"), LastAppliedNumber: 5, LastAppliedAt: ptr(launchTime)}
	got := decode(t, getRocket(t, &fakeRockets{rockets: []domain.Rocket{r}}, "c1"))
	for _, k := range []string{"type", "launchSpeed", "launchedAt", "mission"} {
		if v, present := got[k]; !present || v != nil {
			t.Errorf("%s = %#v (present %v), want null", k, v, present)
		}
	}
	if got["status"] != "exploded" || got["explosionReason"] != "FROZEN_O_RINGS" || got["speed"] != 300.0 {
		t.Errorf("rocket = %v", got)
	}
}

func TestRocketTimes(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"nanoseconds truncated": {"2022-02-02T19:39:05.123456789+01:00", "2022-02-02T18:39:05.123456Z"},
		"year 10000 in UTC":   {"9999-12-31T23:30:00-01:00", "10000-01-01T00:30:00Z"},
		"year -1 in UTC":      {"0000-01-01T00:30:00+01:00", "-0001-12-31T23:30:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339Nano, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			r := launched()
			r.LaunchedAt = &at
			rec := getRocket(t, &fakeRockets{rockets: []domain.Rocket{r}}, "c1")
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if got := decode(t, rec)["launchedAt"]; got != tc.want {
				t.Fatalf("launchedAt = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestRocketNotFound(t *testing.T) {
	repo := &fakeRockets{rockets: []domain.Rocket{launched()}}
	for _, channel := range []string{"c9", "C1"} {
		rec := getRocket(t, repo, channel)
		if rec.Code != http.StatusNotFound || decode(t, rec)["error"] != "rocket not found" {
			t.Errorf("%q: status %d body %s", channel, rec.Code, rec.Body)
		}
	}
	calls := repo.calls
	for _, channel := range []string{"a/b", "€", "a.b", "", "a\n"} { // decoded path values breaking the rule
		rec := getRocket(t, repo, channel)
		if rec.Code != http.StatusNotFound || decode(t, rec)["error"] != "rocket not found" {
			t.Errorf("%q: status %d body %s", channel, rec.Code, rec.Body)
		}
	}
	if repo.calls != calls {
		t.Errorf("repository called %d times for invalid channels", repo.calls-calls)
	}
}

func TestRocketReadFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		repo *fakeRockets
		want int
	}{
		"unavailable": {&fakeRockets{err: errors.Join(handler.ErrUnavailable, errors.New("conn refused"))}, http.StatusServiceUnavailable},
		"timeout":     {&fakeRockets{err: fmt.Errorf("read: %w", context.DeadlineExceeded)}, http.StatusServiceUnavailable},
		"other":       {&fakeRockets{err: errors.New("scan failed")}, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec := getRocket(t, tc.repo, "c1")
			if rec.Code != tc.want || decode(t, rec)["error"] == nil {
				t.Fatalf("status %d body %s, want %d with error", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestRocketClientGoneIsNotAnError(t *testing.T) {
	var logs bytes.Buffer
	h := &handler.Rockets{Repo: &fakeRockets{}, Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/rockets/c1", nil).WithContext(ctx)
	req.SetPathValue("channel", "c1")
	rec := httptest.NewRecorder()
	cancel()
	h.Get(rec, req)
	if rec.Body.Len() != 0 || logs.Len() != 0 {
		t.Fatalf("body %q, logs %q; want neither", rec.Body, logs.String())
	}
}

func rocketOn(channel string) domain.Rocket {
	r := launched()
	r.Channel = channel
	return r
}

func TestRocketsQueryParameters(t *testing.T) {
	for query, want := range map[string]handler.RocketQuery{
		"":                          {Sort: handler.SortChannel, Order: handler.Asc},
		"order=desc":                {Sort: handler.SortChannel, Order: handler.Desc},
		"sort=speed&order=desc":     {Sort: handler.SortSpeed, Order: handler.Desc},
		"sort=type":                 {Sort: handler.SortType, Order: handler.Asc},
		"sort=mission&order=asc":    {Sort: handler.SortMission, Order: handler.Asc},
		"sort=status":               {Sort: handler.SortStatus, Order: handler.Asc},
		"sort=launchedAt":           {Sort: handler.SortLaunchedAt, Order: handler.Asc},
		"sort=channel&page=2&x=%20": {Sort: handler.SortChannel, Order: handler.Asc},
	} {
		repo := &fakeRockets{}
		rec := listRockets(t, repo, query)
		if rec.Code != http.StatusOK || repo.query != want {
			t.Errorf("%q: status %d, query %+v, want %+v", query, rec.Code, repo.query, want)
		}
	}
}

func TestRocketsBadRequest(t *testing.T) {
	for query, param := range map[string]string{
		"sort=altitude":         "sort",
		"sort=Speed":            "sort",
		"sort=":                 "sort",
		"sort":                  "sort",
		"sort=speed&sort=speed": "sort",
		"sort=launched_at":      "sort",
		"order=DESC":            "order",
		"order=":                "order",
		"order=asc&order=desc":  "order",
		"sort=%zz&order=desc":   "invalid URL escape",
		"sort=speed;order=desc": "semicolon",
		"page=1;x":              "semicolon",
	} {
		repo := &fakeRockets{}
		rec := listRockets(t, repo, query)
		msg, _ := decode(t, rec)["error"].(string)
		if rec.Code != http.StatusBadRequest || !strings.Contains(msg, param) || repo.calls != 0 {
			t.Errorf("%q: status %d error %q calls %d, want 400 naming %s", query, rec.Code, msg, repo.calls, param)
		}
	}
	msg, _ := decode(t, listRockets(t, &fakeRockets{}, "sort=x"))["error"].(string)
	if !strings.Contains(msg, "channel, type, mission, speed, status, launchedAt") {
		t.Errorf("error %q does not list the accepted fields", msg)
	}
}

func TestRocketsListAndHidden(t *testing.T) {
	repo := &fakeRockets{rockets: []domain.Rocket{
		rocketOn("b"), rocketOn("a.b"), rocketOn("a"), rocketOn("€"), rocketOn("c"), rocketOn("a\n"),
	}}
	rec := listRockets(t, repo, "sort=speed")
	if got := listed(t, rec); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("channels = %q, want invalid ones dropped and order kept", got)
	}
	single := decode(t, getRocket(t, &fakeRockets{rockets: []domain.Rocket{rocketOn("b")}}, "b"))
	var body struct {
		Rockets []map[string]any `json:"rockets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for k, v := range single {
		if body.Rockets[0][k] != v {
			t.Errorf("list %s = %#v, single = %#v", k, body.Rockets[0][k], v)
		}
	}
}

func TestRocketsEmptyList(t *testing.T) {
	for _, rockets := range [][]domain.Rocket{nil, {}, {rocketOn("a.b")}} {
		rec := listRockets(t, &fakeRockets{rockets: rockets}, "")
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"rockets":[]}` {
			t.Errorf("rockets %v: status %d body %s", rockets, rec.Code, rec.Body)
		}
	}
}

func TestRocketsReadFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		repo *fakeRockets
		want int
	}{
		"unavailable": {&fakeRockets{err: errors.Join(handler.ErrUnavailable, errors.New("conn refused"))}, http.StatusServiceUnavailable},
		"timeout":     {&fakeRockets{err: fmt.Errorf("read: %w", context.DeadlineExceeded)}, http.StatusServiceUnavailable},
		"other":       {&fakeRockets{err: errors.New("scan failed")}, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec := listRockets(t, tc.repo, "")
			if rec.Code != tc.want || decode(t, rec)["error"] == nil {
				t.Fatalf("status %d body %s, want %d with error", rec.Code, rec.Body, tc.want)
			}
		})
	}
}
