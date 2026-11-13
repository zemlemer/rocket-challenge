package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/testdb"
	"sync"
	"testing"
	"time"
)

// fakeRepo records saved entries; identified duplicates are skipped like the real one.
type fakeRepo struct {
	mu      sync.Mutex
	entries []handler.InboxEntry
	seen    map[handler.Identity]bool
	err     error
}

func (f *fakeRepo) Save(_ context.Context, e handler.InboxEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if e.Identity != nil {
		if f.seen == nil {
			f.seen = map[handler.Identity]bool{}
		}
		if f.seen[*e.Identity] {
			return nil
		}
		f.seen[*e.Identity] = true
	}
	f.entries = append(f.entries, e)
	return nil
}

func post(t *testing.T, repo *fakeRepo, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	h := &handler.Messages{Repo: repo, Log: testdb.Logger()}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/messages", body))
	return rec
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func msg(channel, number string) string {
	return `{"metadata":{"channel":` + channel + `,"messageNumber":` + number +
		`,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{}}`
}

func meta(fields string) string { return `{"metadata":{` + fields + `},"message":{"by":1}}` }

// fakeRockets serves rockets from memory; it records the calls it gets and
// fails with the context's error once the context is done.
type fakeRockets struct {
	mu      sync.Mutex
	rockets []domain.Rocket
	err     error
	calls   int
	query   handler.RocketQuery
}

func (f *fakeRockets) Rocket(ctx context.Context, channel string) (domain.Rocket, bool, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Rocket{}, false, err
	}
	if f.err != nil {
		return domain.Rocket{}, false, f.err
	}
	for _, r := range f.rockets {
		if r.Channel == channel {
			return r, true, nil
		}
	}
	return domain.Rocket{}, false, nil
}

func (f *fakeRockets) Rockets(ctx context.Context, q handler.RocketQuery) ([]domain.Rocket, error) {
	f.mu.Lock()
	f.calls++
	f.query = q
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.rockets, nil
}

// getRocket serves GET /rockets/{channel} with channel as the decoded path value.
func getRocket(t *testing.T, repo *fakeRockets, channel string) *httptest.ResponseRecorder {
	t.Helper()
	h := &handler.Rockets{Repo: repo, Log: testdb.Logger()}
	req := httptest.NewRequest(http.MethodGet, "/rockets/x", nil)
	req.SetPathValue("channel", channel)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	return rec
}

// decode unmarshals a JSON response body into a generic map.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

// listRockets serves GET /rockets?<query>.
func listRockets(t *testing.T, repo *fakeRockets, query string) *httptest.ResponseRecorder {
	t.Helper()
	h := &handler.Rockets{Repo: repo, Log: testdb.Logger()}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/rockets?"+query, nil))
	return rec
}

// listed returns the channels of a GET /rockets response, in order.
func listed(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Rockets []struct {
			Channel string `json:"channel"`
		} `json:"rockets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Rockets == nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	out := make([]string, len(body.Rockets))
	for i, r := range body.Rockets {
		out[i] = r.Channel
	}
	return out
}

var launchTime = time.Date(2022, 2, 2, 19, 39, 5, 863370000, time.FixedZone("", 3600))

func launched() domain.Rocket {
	return domain.Rocket{
		Channel: "c1", Speed: 800, RocketType: testdb.Ptr("Falcon-9"), LaunchSpeed: testdb.Ptr(int64(500)),
		LaunchedAt: testdb.Ptr(launchTime), Mission: testdb.Ptr("ARTEMIS"), MissionNumber: 1,
		LastAppliedNumber: 2, LastAppliedAt: testdb.Ptr(launchTime.Add(time.Second)),
	}
}

func rocketOn(channel string) domain.Rocket {
	r := launched()
	r.Channel = channel
	return r
}
