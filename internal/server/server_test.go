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
	routes := server.Routes(server.Handlers{Messages: ok(http.StatusAccepted), Health: ok(http.StatusOK)})

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
