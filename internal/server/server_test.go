package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
