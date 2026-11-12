package helpers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"rockets/pkg/helpers"
)

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	helpers.WriteError(rec, http.StatusTeapot, "boom")
	if rec.Code != http.StatusTeapot || rec.Header().Get("Content-Type") != "application/json" ||
		rec.Body.String() != "{\"error\":\"boom\"}\n" {
		t.Fatalf("status=%d ct=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}
