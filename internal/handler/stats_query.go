package handler

import (
	"context"
	"time"
)

// InboxTotals counts every inbox entry, with or without identity.
type InboxTotals struct {
	Received    int64
	Pending     int64
	Applied     int64
	Quarantined int64
	Failed      int64
	// OldestPendingAt is the receive time of the oldest unprocessed entry;
	// nil when nothing is pending.
	OldestPendingAt *time.Time
}

// ChannelStats counts the entries of one channel numbered 1 or higher.
// Identities are unique, so Received is the number of distinct numbers.
type ChannelStats struct {
	Channel     string
	Highest     int64 // highest message number
	Received    int64
	Pending     int64
	Applied     int64
	Quarantined int64
	Failed      int64
}

// StatsSnapshot is one consistent view of intake and processing progress.
type StatsSnapshot struct {
	AsOf     time.Time // database time of the snapshot
	Inbox    InboxTotals
	Channels []ChannelStats // every channel, unfiltered and in any order
}

// StatsRepository is what the stats handler needs from storage. It does not
// apply the channel rule, which the handler owns (design D4).
type StatsRepository interface {
	Stats(ctx context.Context) (StatsSnapshot, error)
}
