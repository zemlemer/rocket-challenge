package handler_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockets/internal/handler"
)

func TestEveryContentIsAccepted(t *testing.T) {
	valid := msg(`"c1"`, `1`)
	for name, body := range map[string]string{
		"valid":            valid,
		"malformed JSON":   `{"metadata":`,
		"unknown type":     strings.Replace(valid, "RocketLaunched", "RocketTeleported", 1),
		"missing identity": `{"message":{"by":3}}`,
		"empty body":       ``,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			rec := post(t, repo, strings.NewReader(body))
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", rec.Code)
			}
			if len(repo.entries) != 1 || string(repo.entries[0].Body) != body {
				t.Fatalf("stored %+v, want the body as is", repo.entries)
			}
		})
	}
}

func TestMetadataIsStoredWithBodyUnchanged(t *testing.T) {
	repo := &fakeRepo{}
	body := msg(`"c1"`, `5`)
	post(t, repo, strings.NewReader(body))
	e := repo.entries[0]
	if e.Identity == nil || *e.Identity != (handler.Identity{Channel: "c1", MessageNumber: 5}) {
		t.Fatalf("identity = %+v", e.Identity)
	}
	if e.MessageType == nil || *e.MessageType != "RocketLaunched" || e.MessageTime == nil {
		t.Fatalf("type = %v, time = %v", e.MessageType, e.MessageTime)
	}
	if string(e.Body) != body {
		t.Fatalf("body changed: %q", e.Body)
	}
}

func TestDuplicatesAreAccepted(t *testing.T) {
	repo := &fakeRepo{}
	for range 3 {
		if rec := post(t, repo, strings.NewReader(msg(`"c1"`, `1`))); rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if len(repo.entries) != 1 {
		t.Fatalf("stored %d entries, want 1", len(repo.entries))
	}
}

func TestBodySizeBoundary(t *testing.T) {
	t.Run("exactly 1 MiB is complete", func(t *testing.T) {
		repo := &fakeRepo{}
		rec := post(t, repo, strings.NewReader(strings.Repeat("x", handler.MaxBodyBytes)))
		e := repo.entries[0]
		if rec.Code != http.StatusAccepted || e.Truncated || len(e.Body) != handler.MaxBodyBytes {
			t.Fatalf("status=%d truncated=%v len=%d", rec.Code, e.Truncated, len(e.Body))
		}
	})
	t.Run("1 MiB + 1 is truncated", func(t *testing.T) {
		repo := &fakeRepo{}
		post(t, repo, strings.NewReader(strings.Repeat("x", handler.MaxBodyBytes+1)))
		if e := repo.entries[0]; !e.Truncated || len(e.Body) != handler.MaxBodyBytes {
			t.Fatalf("truncated=%v len=%d", e.Truncated, len(e.Body))
		}
	})
	t.Run("2 MiB message is truncated and has no identity", func(t *testing.T) {
		repo := &fakeRepo{}
		big := msg(`"c1"`, `1`) + strings.Repeat(" ", 2*handler.MaxBodyBytes)
		rec := post(t, repo, strings.NewReader(big))
		e := repo.entries[0]
		if rec.Code != http.StatusAccepted || !e.Truncated || e.Identity != nil || len(e.Body) != handler.MaxBodyBytes {
			t.Fatalf("status=%d truncated=%v identity=%v len=%d", rec.Code, e.Truncated, e.Identity, len(e.Body))
		}
	})
}

func TestAbortedBodyStoresNothing(t *testing.T) {
	repo := &fakeRepo{}
	rec := post(t, repo, io.MultiReader(strings.NewReader(msg(`"c1"`, `1`)[:20]), failingReader{}))
	if rec.Code < 500 {
		t.Fatalf("status = %d, want 5xx", rec.Code)
	}
	if len(repo.entries) != 0 {
		t.Fatalf("stored %d entries from an aborted body", len(repo.entries))
	}
}

func TestRepositoryErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"unavailable": {handler.ErrUnavailable, http.StatusServiceUnavailable},
		"other":       {errors.New("boom"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec := post(t, &fakeRepo{err: tc.err}, strings.NewReader(msg(`"c1"`, `1`)))
			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), `"error"`) {
				t.Fatalf("status = %d body = %s, want %d with error", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestHealthIsLiveness(t *testing.T) {
	rec := httptest.NewRecorder()
	handler.Health{}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("status = %d body = %s, want 200 ok", rec.Code, rec.Body)
	}
}
