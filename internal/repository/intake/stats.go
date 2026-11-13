package intake

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/handler"
)

// Stats reads intake and processing progress from the inbox. Its query
// takes no locks, so it never blocks intake or processing.
type Stats struct {
	pool *pgxpool.Pool
}

var _ handler.StatsRepository = (*Stats)(nil)

// NewStats returns a stats repository over pool.
func NewStats(pool *pgxpool.Pool) *Stats { return &Stats{pool: pool} }

// statsQuery returns the totals row (grouping set (), is_total true) and one
// row per channel in one scan. ch is the channel of an entry numbered 1 or
// higher and null otherwise, so the channel rows count only such entries
// while the totals count every entry. The null ch group (entries without a
// per-channel row) is dropped by the having clause. max(message_number) is
// meaningful only for channel rows, and the oldest pending receive time only
// for the totals; id order need not match receive order.
const statsQuery = `
	select grouping(ch) = 1 as is_total,
	       ch,
	       now(),
	       max(message_number),
	       count(*),
	       count(*) filter (where processed_at is null),
	       count(*) filter (where outcome = 'applied'),
	       count(*) filter (where outcome = 'quarantined'),
	       count(*) filter (where outcome = 'failed'),
	       min(received_at) filter (where processed_at is null)
	from (
		select case when message_number >= 1 then channel end as ch,
		       message_number, processed_at, outcome, received_at
		from inbox
	) i
	group by grouping sets ((), (ch))
	having grouping(ch) = 1 or ch is not null`

// Stats takes the report from one statement, so totals and channel rows come
// from the same snapshot under the default isolation level.
func (r *Stats) Stats(ctx context.Context) (handler.StatsSnapshot, error) {
	var s handler.StatsSnapshot
	rows, err := r.pool.Query(ctx, statsQuery)
	if err != nil {
		return s, classify("read stats", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			total   bool
			channel *string
			highest *int64
			c       handler.ChannelStats
			in      handler.InboxTotals
		)
		if err := rows.Scan(&total, &channel, &s.AsOf, &highest,
			&c.Received, &c.Pending, &c.Applied, &c.Quarantined, &c.Failed, &in.OldestPendingAt); err != nil {
			return s, classify("read stats", err)
		}
		if total {
			in.Received, in.Pending, in.Applied, in.Quarantined, in.Failed = c.Received, c.Pending, c.Applied, c.Quarantined, c.Failed
			s.Inbox = in
			continue
		}
		c.Channel, c.Highest = *channel, *highest
		s.Channels = append(s.Channels, c)
	}
	if err := rows.Err(); err != nil {
		return s, classify("read stats", err)
	}
	return s, nil
}
