// Package domain holds the rocket business rules: which inbox entries are
// conforming messages and how messages combine into rocket state. It is pure
// Go with no I/O, so every rule is testable without a database.
package domain

import "time"

// Message types.
const (
	TypeLaunched       = "RocketLaunched"
	TypeSpeedIncreased = "RocketSpeedIncreased"
	TypeSpeedDecreased = "RocketSpeedDecreased"
	TypeExploded       = "RocketExploded"
	TypeMissionChanged = "RocketMissionChanged"
)

// MaxAmount bounds launch speeds and speed changes. With it, summing the
// speed of one rocket cannot overflow int64 in practice (it would take more
// than nine billion messages).
const MaxAmount = 1_000_000_000

// InboxEntry is one unprocessed inbox row as the processor sees it: metadata
// parsed by intake (nil when it was malformed) and the raw body.
type InboxEntry struct {
	ID            int64
	Channel       *string
	MessageNumber *int64
	MessageTime   *time.Time
	MessageType   *string
	Body          []byte
	Truncated     bool
}

// Message is a conforming rocket message. Only the payload fields of its
// Type are set.
type Message struct {
	Channel string
	Number  int64
	Time    time.Time
	Type    string

	RocketType  string // RocketLaunched
	LaunchSpeed int64  // RocketLaunched
	Mission     string // RocketLaunched, RocketMissionChanged (newMission)
	By          int64  // RocketSpeedIncreased, RocketSpeedDecreased
	Reason      string // RocketExploded
}

// Rejection explains why an entry is not a conforming message.
type Rejection struct{ Reason string }

func (r *Rejection) Error() string { return r.Reason }

func reject(format string, args ...any) error { return &Rejection{Reason: sprintf(format, args...)} }
