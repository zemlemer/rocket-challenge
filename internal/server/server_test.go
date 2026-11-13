package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rockets/internal/server"
)

func TestRoutes(t *testing.T) {
	ok := func(code int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
	}
	routes := server.Routes(server.Handlers{
		Messages: ok(http.StatusAccepted), Health: ok(http.StatusOK),
		Rockets: ok(http.StatusOK), Rocket: ok(http.StatusTeapot), Stats: ok(http.StatusNonAuthoritativeInfo),
	})

	for _, tc := range []struct {
		method, path string
		want         int
		jsonError    bool
	}{
		{"POST", "/messages", http.StatusAccepted, false},
		{"GET", "/healthz", http.StatusOK, false},
		{"GET", "/messages", http.StatusMethodNotAllowed, true},
		{"PUT", "/messages", http.StatusMethodNotAllowed, true},
		{"POST", "/healthz", http.StatusMethodNotAllowed, true},
		{"GET", "/nowhere", http.StatusNotFound, true},
		{"POST", "/messages/extra", http.StatusNotFound, true},
		{"GET", "/rockets", http.StatusOK, false},
		{"HEAD", "/rockets", http.StatusOK, false},
		{"GET", "/rockets/c1", http.StatusTeapot, false},
		{"HEAD", "/rockets/c1", http.StatusTeapot, false},
		{"POST", "/rockets", http.StatusMethodNotAllowed, true},
		{"DELETE", "/rockets/c1", http.StatusMethodNotAllowed, true},
		{"GET", "/rockets/", http.StatusNotFound, true},
		{"GET", "/rockets/c1/x", http.StatusNotFound, true},
		{"GET", "/stats", http.StatusNonAuthoritativeInfo, false},
		{"HEAD", "/stats", http.StatusNonAuthoritativeInfo, false},
		{"GET", "/stats?x=1", http.StatusNonAuthoritativeInfo, false},
		{"POST", "/stats", http.StatusMethodNotAllowed, true},
		{"GET", "/stats/x", http.StatusNotFound, true},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if !tc.jsonError {
				return
			}
			if tc.want == http.StatusMethodNotAllowed && rec.Header().Get("Allow") == "" {
				t.Fatal("405 without Allow")
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("body = %s, want {\"error\": ...}", rec.Body)
			}
		})
	}
}

func TestRocketRoutePathHandling(t *testing.T) {
	var got string
	none := http.NotFoundHandler()
	routes := server.Routes(server.Handlers{Messages: none, Health: none, Rockets: none, Stats: none,
		Rocket: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.PathValue("channel") })})
	for path, want := range map[string]string{"/rockets/a%2Fb": "a/b", "/rockets/%E2%82%AC": "€", "/rockets/c%31": "c1"} {
		got = ""
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if got != want {
			t.Errorf("%s: channel %q, want %q", path, got, want)
		}
	}
	for _, path := range []string{"/rockets/./c1", "/rockets//c1"} {
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/rockets/c1" {
			t.Errorf("%s: status %d location %q, want 307 to /rockets/c1", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/rockets", "/stats"} {
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, path, nil))
		if rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: Allow = %q", path, rec.Header().Get("Allow"))
		}
	}
}

// On shutdown timeout Run calls abort, closes the connections and returns
// ErrUncleanShutdown.
func TestRunAbortsWorkAfterShutdownTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inHandler := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	routes := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(inHandler)
		<-release
	})
	ctx, stop := context.WithCancel(context.Background())
	aborted := make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- server.Run(ctx, l, routes, 200*time.Millisecond, func() { close(aborted) }, slog.New(slog.DiscardHandler))
	}()

	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + l.Addr().String() + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()
	<-inHandler
	stop()

	select {
	case err := <-errc:
		if !errors.Is(err, server.ErrUncleanShutdown) {
			t.Fatalf("Run = %v, want ErrUncleanShutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the shutdown timeout")
	}
	select {
	case <-aborted:
	default:
		t.Fatal("abort not called")
	}
	if err := <-clientErr; err == nil {
		t.Fatal("client got a response; connection should have been closed")
	}
}

func TestRunCleanShutdownDoesNotAbort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	abort := func() { t.Error("abort called on a clean shutdown") }
	if err := server.Run(ctx, l, http.NotFoundHandler(), time.Second, abort, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}
