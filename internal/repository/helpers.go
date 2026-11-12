package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"rockets/internal/handler"
)

// unavailableCodes are server errors meaning "try again later", not a bug.
var unavailableCodes = map[string]bool{
	"53300": true, // too_many_connections
	"57P01": true, // admin_shutdown
	"57P02": true, // crash_shutdown
	"57P03": true, // cannot_connect_now
}

// classify wraps storage errors that the sender can overcome by redelivery
// with handler.ErrUnavailable. Errors reported by PostgreSQL itself for the
// statement (constraint violations, invalid data) stay unwrapped: they are
// bugs, not outages.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && !unavailableCodes[pgErr.Code] && pgErr.Code[:2] != "08" {
		return fmt.Errorf("%s: %w", op, err)
	}
	// Connection failures, timeouts, closed pool, connection-class errors.
	return fmt.Errorf("%s: %w: %w", op, handler.ErrUnavailable, err)
}
