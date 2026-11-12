package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"rockets/internal/handler"
	"rockets/internal/testdb"
	"sync"
	"testing"
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
