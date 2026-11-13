package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockets/internal/handler"
	"rockets/internal/repository/intake"
	"rockets/internal/testdb"
)

// Spec message-intake "Recovery from storage outage": the server keeps its
// pool across a database outage — 503 during, 202 after, no pool
// re-creation — and the redelivered message is stored once.
func TestIntakeRecoversFromDatabaseOutage(t *testing.T) {
	url, _ := testdb.Migrated(t)
	proxy, proxied := testdb.NewProxy(t, url)
	repo := intake.NewInbox(testdb.Pool(t, proxied))
	messages := &handler.Messages{Repo: repo, Log: testdb.Logger()}
	health := handler.Health{}

	const body = `{"metadata":{"channel":"c1","messageNumber":1},"message":{}}`
	do := func(h http.Handler, method string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", strings.NewReader(body)))
		return rec.Code
	}

	if got := do(messages, http.MethodPost); got != http.StatusAccepted {
		t.Fatalf("before outage: %d", got)
	}

	proxy.Stop()
	if got := do(messages, http.MethodPost); got != http.StatusServiceUnavailable {
		t.Fatalf("during outage POST: %d, want 503", got)
	}
	if got := do(health, http.MethodGet); got != http.StatusOK {
		t.Fatalf("during outage /healthz: %d, want 200 (liveness, no database check)", got)
	}

	proxy.Start()
	if got := do(messages, http.MethodPost); got != http.StatusAccepted {
		t.Fatalf("after outage POST: %d, want 202", got)
	}

	var n int
	if err := testdb.Pool(t, url).QueryRow(context.Background(), "select count(*) from inbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("inbox holds %d entries, want 1", n)
	}
}
