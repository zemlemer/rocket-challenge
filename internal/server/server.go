// Package server assembles the HTTP server: routes, timeouts, JSON errors for
// unknown routes, and graceful shutdown. It knows handlers, not storage.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"rockets/pkg/helpers"
	"time"
)

// Handlers are the endpoints served.
type Handlers struct {
	Messages http.Handler // POST /messages
	Health   http.Handler // GET /healthz
}

// Routes returns the router. Unknown paths get a JSON 404, known paths with
// another method a JSON 405.
func Routes(h Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /messages", h.Messages)
	mux.Handle("/messages", methodNotAllowed("POST"))
	mux.Handle("GET /healthz", h.Health)
	mux.Handle("/healthz", methodNotAllowed("GET, HEAD"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		helpers.WriteError(w, http.StatusNotFound, "not found: "+r.URL.Path)
	})
	return mux
}

// Run serves on l until ctx is cancelled, then shuts down gracefully: no new
// connections, in-flight requests finish within shutdownTimeout. If they do
// not, the remaining connections are closed and an error is returned.
func Run(ctx context.Context, l net.Listener, routes http.Handler, shutdownTimeout time.Duration, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	log.Info("listening", "addr", l.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", shutdownTimeout)
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown was not clean: requests still in flight after %s: %w", shutdownTimeout, err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
