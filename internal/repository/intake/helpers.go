package intake

import (
	"fmt"

	"rockets/internal/handler"
	"rockets/internal/postgres"
)

// classify wraps an intake storage error with handler.ErrUnavailable when
// the sender can overcome it by redelivery.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	if postgres.IsUnavailable(err) {
		return fmt.Errorf("%s: %w: %w", op, handler.ErrUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
