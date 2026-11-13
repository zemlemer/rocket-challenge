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
			if repo.entries[0].Truncated {
				t.Fatal("body within the size limit was marked truncated")
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
	for _, tc := range []struct {
		name string
		size int
	}{
		{"exactly 1 MiB", handler.MaxBodyBytes},
		{"1 MiB + 1 byte", handler.MaxBodyBytes + 1},
		{"2 MiB", 2 * handler.MaxBodyBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			prefix := msg(`"c1"`, `1`)
			body := prefix + strings.Repeat(" ", tc.size-len(prefix))
			rec := post(t, repo, strings.NewReader(body))
			if rec.Code != http.StatusAccepted || len(repo.entries) != 1 {
				t.Fatalf("status = %d, stored %d entries", rec.Code, len(repo.entries))
			}
			e := repo.entries[0]
			if tc.size == handler.MaxBodyBytes {
				if e.Truncated || string(e.Body) != body || e.Identity == nil {
					t.Fatalf("exact-limit entry: truncated=%v len=%d identity=%v", e.Truncated, len(e.Body), e.Identity)
				}
				return
			}
			if !e.Truncated || len(e.Body) != 0 || e.Metadata != (handler.Metadata{}) {
				t.Fatalf("oversized entry: truncated=%v len=%d metadata=%+v", e.Truncated, len(e.Body), e.Metadata)
			}
		})
	}
}

func TestAbortedBodyStoresNothing(t *testing.T) {
	for name, partial := range map[string]string{
		"below limit": msg(`"c1"`, `1`)[:20],
		"above limit": strings.Repeat("x", handler.MaxBodyBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			rec := post(t, repo, io.MultiReader(strings.NewReader(partial), failingReader{}))
			if rec.Code < 500 {
				t.Fatalf("status = %d, want 5xx", rec.Code)
			}
			if len(repo.entries) != 0 {
				t.Fatalf("stored %d entries from an aborted body", len(repo.entries))
			}
		})
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
