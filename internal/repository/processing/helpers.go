package processing

import (
	"fmt"

	"rockets/internal/postgres"
	"rockets/internal/processor"
)

// classify wraps a processing storage error with processor.ErrUnavailable
// when the batch can succeed after a pause.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	if postgres.IsUnavailable(err) {
		return fmt.Errorf("%s: %w: %w", op, processor.ErrUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
