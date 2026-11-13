package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/handler"
)

// Stats reads intake and processing progress from the inbox. Its queries
// take no locks, so they never block intake or processing.
type Stats struct {
	pool *pgxpool.Pool
}

var _ handler.StatsRepository = (*Stats)(nil)

// NewStats returns a stats repository over pool.
func NewStats(pool *pgxpool.Pool) *Stats { return &Stats{pool: pool} }

const (
	statsTotals = `
		select now(),
		       count(*),
		       count(*) filter (where processed_at is null),
		       count(*) filter (where outcome = 'applied'),
		       count(*) filter (where outcome = 'quarantined'),
		       count(*) filter (where outcome = 'failed')
		from inbox`

	// Ids grow with arrival; the partial index makes this one index probe.
	statsOldestPending = `
		select received_at from inbox where processed_at is null order by id limit 1`

	statsChannels = `
		select channel,
		       max(message_number),
		       count(*),
		       count(*) filter (where processed_at is null),
		       count(*) filter (where outcome = 'applied'),
		       count(*) filter (where outcome = 'quarantined'),
		       count(*) filter (where outcome = 'failed')
		from inbox
		where channel is not null and message_number >= 1
		group by channel`
)

// Stats takes the report in one read-only snapshot (design D2).
func (r *Stats) Stats(ctx context.Context) (handler.StatsSnapshot, error) {
	var s handler.StatsSnapshot
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return s, classify("read stats", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	in := &s.Inbox
	if err := tx.QueryRow(ctx, statsTotals).Scan(&s.AsOf, &in.Received, &in.Pending, &in.Applied, &in.Quarantined, &in.Failed); err != nil {
		return s, classify("read stats", err)
	}
	if in.Pending > 0 {
		if err := tx.QueryRow(ctx, statsOldestPending).Scan(&in.OldestPendingAt); err != nil {
			return s, classify("read stats", err)
		}
	}
	rows, err := tx.Query(ctx, statsChannels)
	if err != nil {
		return s, classify("read stats", err)
	}
	s.Channels, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (handler.ChannelStats, error) {
		var c handler.ChannelStats
		err := row.Scan(&c.Channel, &c.Highest, &c.Received, &c.Pending, &c.Applied, &c.Quarantined, &c.Failed)
		return c, err
	})
	if err != nil {
		return s, classify("read stats", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return s, classify("read stats", err)
	}
	return s, nil
}
