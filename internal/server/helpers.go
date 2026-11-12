package server

import (
	"net/http"

	"rockets/pkg/helpers"
)

// methodNotAllowed answers a known path called with another method.
func methodNotAllowed(allow string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		helpers.WriteError(w, http.StatusMethodNotAllowed, "method "+r.Method+" not allowed")
	})
}
