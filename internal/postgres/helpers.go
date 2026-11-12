package postgres

import "time"

const pingTimeout = 2 * time.Second

// latestVersion returns the highest version in ms (0 if empty); ms is sorted.
func latestVersion(ms []Migration) int64 {
	if len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].Version
}
