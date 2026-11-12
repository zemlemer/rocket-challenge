package helpers

import "time"

const (
	backoffInitial = 100 * time.Millisecond
	backoffMax     = 2 * time.Second
)

// Backoff returns the delay before retry number attempt (0-based):
// 100ms doubling per attempt, capped at 2s.
func Backoff(attempt int) time.Duration {
	d := backoffInitial
	for i := 0; i < attempt && d < backoffMax; i++ {
		d *= 2
	}
	return min(d, backoffMax)
}
