package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"rockets/data"
	"rockets/internal/handler"
	"rockets/internal/postgres"
	"rockets/internal/repository/intake"
	"rockets/internal/server"
)

// ServerCmd serves the HTTP API: it stores incoming messages in the inbox.
type ServerCmd struct {
	HTTPAddr        string        `name:"http-addr" env:"HTTP_ADDR" default:":8088" help:"Listen address."`
	ShutdownTimeout time.Duration `name:"shutdown-timeout" env:"SHUTDOWN_TIMEOUT" default:"15s" help:"How long in-flight requests may run after SIGINT/SIGTERM."`
}

func (ServerCmd) needsDatabase() {}

// Run connects, checks the schema version, and serves until ctx is cancelled.
// Inserts run on a server-lifetime context that only an unclean shutdown
// cancels; closing the pool afterwards is then bounded by the same timeout.
func (c *ServerCmd) Run(ctx context.Context, g *Globals, log *slog.Logger) (err error) {
	host, _ := os.Hostname()
	log = log.With("instance", host)

	pool, err := postgres.Connect(ctx, g.DatabaseURL, g.DBConnectTimeout, log)
	if err != nil {
		return err
	}
	defer func() {
		if !errors.Is(err, server.ErrUncleanShutdown) {
			pool.Close()
			return
		}
		if !closeWithin(pool.Close, c.ShutdownTimeout) {
			log.Warn("database pool still not closed; exiting anyway", "timeout", c.ShutdownTimeout)
		}
	}()

	required, err := postgres.LatestVersion(data.Migrations())
	if err != nil {
		return err
	}
	if err := postgres.RequireSchema(ctx, pool, required); err != nil {
		return err
	}

	l, err := net.Listen("tcp", c.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", c.HTTPAddr, err)
	}

	work, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()

	inbox := intake.NewInbox(pool)
	rockets := &handler.Rockets{Repo: intake.NewRockets(pool), Log: log}
	routes := server.Routes(server.Handlers{
		Messages: &handler.Messages{Repo: inbox, Log: log, Work: work},
		Health:   handler.Health{},
		Rockets:  http.HandlerFunc(rockets.List),
		Rocket:   http.HandlerFunc(rockets.Get),
		Stats:    &handler.Stats{Repo: intake.NewStats(pool), Log: log},
	})
	return server.Run(ctx, l, routes, c.ShutdownTimeout, abort, log)
}
