package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alecthomas/kong"
)

// usageError prints err and the usage of the selected command to stderr and
// returns the usage exit status.
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
