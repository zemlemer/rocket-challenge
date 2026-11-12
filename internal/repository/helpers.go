package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"rockets/internal/handler"
	"rockets/internal/processor"
)

// unavailableCodes are server errors meaning "try again later", not a bug.
var unavailableCodes = map[string]bool{
	"53300": true, // too_many_connections
	"57P01": true, // admin_shutdown
	"57P02": true, // crash_shutdown
	"57P03": true, // cannot_connect_now
	"55P03": true, // lock_not_available (lock_timeout)
	"57014": true, // query_canceled (statement_timeout)
	"25P03": true, // idle_in_transaction_session_timeout
}

// isUnavailable reports whether err is an outage worth retrying after a
// pause rather than an error of the statement itself. Errors that PostgreSQL
// reports for a statement (constraint violations, invalid data) are bugs;
// everything else — connection failures, timeouts, closed pool — is not.
func isUnavailable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return unavailableCodes[pgErr.Code] || pgErr.Code[:2] == "08"
	}
	return true
}

// classify wraps an intake storage error with handler.ErrUnavailable when
// the sender can overcome it by redelivery.
func classify(op string, err error) error {
	return wrap(op, err, handler.ErrUnavailable)
}

// classifyProcessing does the same for the processor.
func classifyProcessing(op string, err error) error {
	return wrap(op, err, processor.ErrUnavailable)
}

func wrap(op string, err, unavailable error) error {
	if err == nil {
		return nil
	}
	if isUnavailable(err) {
		return fmt.Errorf("%s: %w: %w", op, unavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
