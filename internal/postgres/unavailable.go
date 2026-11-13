package postgres

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"
)

// unavailableCodes are server errors meaning "try again later", not a bug.
// Class 08 (connection exception) is matched separately.
var unavailableCodes = map[string]bool{
	"53300": true, // too_many_connections
	"57P01": true, // admin_shutdown
	"57P02": true, // crash_shutdown
	"57P03": true, // cannot_connect_now
	"57P05": true, // idle_session_timeout
	"55P03": true, // lock_not_available (lock_timeout)
	"57014": true, // query_canceled (statement_timeout)
	"25P03": true, // idle_in_transaction_session_timeout
	// Concurrency conflicts: the transaction was rolled back as a whole and
	// succeeds when run again, like a lock_timeout. Marking an entry failed
	// for them would drop a valid message's effect.
	"40P01": true, // deadlock_detected
	"40001": true, // serialization_failure
}

// IsUnavailable reports whether err is an outage worth retrying after a
// pause rather than an error of the statement itself: a transient server
// error code, a connection failure, a timeout or a closed pool. Everything
// else — constraint violations, invalid data, but also client-side errors
// such as failing to scan a row — is a bug, so that processing isolates the
// entry instead of retrying the same batch forever (design D5a of
// add-message-processing).
func IsUnavailable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return unavailableCodes[pgErr.Code] || strings.HasPrefix(pgErr.Code, "08")
	}
	var (
		netErr     net.Error
		connectErr *pgconn.ConnectError
	)
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		pgconn.Timeout(err) ||
		pgconn.SafeToRetry(err) || // failed before anything was sent
		errors.Is(err, pgconn.ErrConnClosed) ||
		errors.Is(err, puddle.ErrClosedPool) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || // server closed the connection
		errors.As(err, &netErr) ||
		errors.As(err, &connectErr)
}
