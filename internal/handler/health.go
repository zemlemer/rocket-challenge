package handler

import (
	"net/http"

	"rockets/pkg/helpers"
)

// Health handles GET /healthz: a liveness check. It answers 200 whenever the
// process can serve HTTP and deliberately does not touch the database —
// storage problems surface per request as 503 on POST /messages, and the
// deployment starts servers only after the database is healthy.
type Health struct{}

func (Health) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	helpers.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
