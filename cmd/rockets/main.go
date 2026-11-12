// Command rockets is the single executable of the rockets system.
// Its operating mode is selected by a subcommand, e.g. `rockets migrate`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
)

// Exit statuses of the executable.
const (
	exitOK    = 0
	exitError = 1 // the mode ran and failed
	exitUsage = 2 // invalid invocation
)

// Globals are options shared by all modes.
type Globals struct {
	DatabaseURL      string        `name:"database-url" env:"DATABASE_URL" help:"PostgreSQL connection URL."`
	DBConnectTimeout time.Duration `name:"db-connect-timeout" env:"DB_CONNECT_TIMEOUT" default:"30s" help:"How long to retry the initial database connection."`
	LogLevel         string        `name:"log-level" env:"LOG_LEVEL" default:"info" enum:"debug,info,warn,error" help:"Log level (${enum})."`
}

// CLI is the command-line interface: global options plus one field per mode.
type CLI struct {
	Globals

	Migrate MigrateCmd `cmd:"" help:"Bring the database schema to the latest version."`
}

// needsDatabase is implemented by modes that require DATABASE_URL.
type needsDatabase interface{ needsDatabase() }

// exitRequest carries an exit status requested by kong (e.g. after --help).
type exitRequest struct{ code int }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("rockets"),
		kong.Description("Rockets system: one executable, several modes."),
		kong.Writers(stdout, stderr),
		// kong calls Exit for --help; turn it into a return value instead of os.Exit.
		kong.Exit(func(c int) { panic(exitRequest{c}) }),
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "rockets: internal error: %v\n", err)
		return exitError
	}

	defer func() {
		if r := recover(); r != nil {
			req, ok := r.(exitRequest)
			if !ok {
				panic(r)
			}
			code = req.code
		}
	}()

	kctx, err := parser.Parse(args)
	if err != nil {
		// kong's own usage exit status is 80; the spec requires 2.
		return usageError(stderr, parser, kctx, err)
	}
	if _, ok := kctx.Selected().Target.Addr().Interface().(needsDatabase); ok && cli.DatabaseURL == "" {
		return usageError(stderr, parser, kctx, errors.New("database URL is required: set DATABASE_URL or pass --database-url"))
	}

	log := newLogger(stderr, cli.LogLevel)
	kctx.BindTo(ctx, (*context.Context)(nil))
	if err := kctx.Run(&cli.Globals, log); err != nil {
		log.Error(kctx.Command()+" failed", "error", err)
		return exitError
	}
	return exitOK
}

func usageError(stderr io.Writer, parser *kong.Kong, kctx *kong.Context, err error) int {
	_, _ = fmt.Fprintf(stderr, "rockets: error: %v\n\n", err)
	var perr *kong.ParseError
	if kctx == nil && errors.As(err, &perr) {
		kctx = perr.Context
	}
	if kctx != nil {
		parser.Stdout = stderr // usage after an error goes to stderr
		_ = kctx.PrintUsage(true)
	}
	return exitUsage
}

func newLogger(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(strings.ToUpper(level)))
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l}))
}
