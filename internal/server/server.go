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
	Rockets  http.Handler // GET /rockets
	Rocket   http.Handler // GET /rockets/{channel}
	Stats    http.Handler // GET /stats
}

// Routes returns the router. Unknown paths get a JSON 404, known paths with
// another method a JSON 405.
func Routes(h Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /messages", h.Messages)
	mux.Handle("/messages", methodNotAllowed("POST"))
	mux.Handle("GET /healthz", h.Health)
	mux.Handle("/healthz", methodNotAllowed("GET, HEAD"))
	mux.Handle("GET /rockets", h.Rockets)
	mux.Handle("/rockets", methodNotAllowed("GET, HEAD"))
	mux.Handle("GET /rockets/{channel}", h.Rocket)
	mux.Handle("/rockets/{channel}", methodNotAllowed("GET, HEAD"))
	mux.Handle("GET /stats", h.Stats)
	mux.Handle("/stats", methodNotAllowed("GET, HEAD"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		helpers.WriteError(w, http.StatusNotFound, "not found: "+r.URL.Path)
	})
	return mux
}

// ErrUncleanShutdown is wrapped by the error Run returns when requests were
// still in flight after the shutdown timeout.
var ErrUncleanShutdown = errors.New("shutdown was not clean")

// Run serves on l until ctx is cancelled, then shuts down gracefully: no new
// connections, in-flight requests finish within shutdownTimeout. If they do
// not, abort (if not nil) is called to cancel their remaining work, the
// connections are closed and an error wrapping ErrUncleanShutdown is returned.
func Run(ctx context.Context, l net.Listener, routes http.Handler, shutdownTimeout time.Duration, abort func(), log *slog.Logger) error {
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
		if abort != nil {
			abort()
		}
		_ = srv.Close()
		return fmt.Errorf("%w: requests still in flight after %s: %w", ErrUncleanShutdown, shutdownTimeout, err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
