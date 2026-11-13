package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"
)

func TestIsUnavailable(t *testing.T) {
	pg := func(code string) error { return fmt.Errorf("exec: %w", &pgconn.PgError{Code: code}) }
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Transient server errors: retry the batch.
		{"connection exception class 08", pg("08006"), true},
		{"too many connections", pg("53300"), true},
		{"admin shutdown", pg("57P01"), true},
		{"idle session timeout", pg("57P05"), true},
		{"lock timeout", pg("55P03"), true},
		{"statement timeout", pg("57014"), true},
		{"idle in transaction timeout", pg("25P03"), true},
		{"deadlock", pg("40P01"), true},
		{"serialization failure", pg("40001"), true},

		// Connection and timeout failures on the client side.
		{"context deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), true},
		{"context canceled", context.Canceled, true},
		{"connection closed", fmt.Errorf("commit: %w", pgconn.ErrConnClosed), true},
		{"closed pool", fmt.Errorf("acquire: %w", puddle.ErrClosedPool), true},
		{"server closed connection", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		{"eof", io.EOF, true},
		{"network error", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true},
		{"connect error", &pgconn.ConnectError{Config: &pgconn.Config{}}, true},

		// Bugs: isolate the entry instead of retrying forever.
		{"unique violation", pg("23505"), false},
		{"check violation", pg("23514"), false},
		{"numeric out of range", pg("22003"), false},
		{"invalid utf-8", pg("22021"), false},
		{"empty code", pg(""), false},
		{"scan error", pgx.ScanArgError{ColumnIndex: 2, Err: errors.New("cannot scan")}, false},
		{"plain error", errors.New("unexpected"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnavailable(tt.err); got != tt.want {
				t.Errorf("isUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
