package postgres

import "time"

const (
	backoffInitial = 100 * time.Millisecond
	backoffMax     = 2 * time.Second
	pingTimeout    = 2 * time.Second
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

// latestVersion returns the highest version in ms (0 if empty); ms is sorted.
func latestVersion(ms []Migration) int64 {
	if len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].Version
}
