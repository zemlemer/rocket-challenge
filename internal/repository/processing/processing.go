// Package processing implements the processor's repository port on PostgreSQL.
package processing

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/postgres"
	"rockets/internal/processor"
)

// Repository applies inbox entries to rocket state.
type Repository struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

var _ processor.Repository = (*Repository)(nil)

// New returns a processing repository over pool.
func New(pool *pgxpool.Pool, log *slog.Logger) *Repository {
	return &Repository{pool: pool, log: log}
}

// ProcessBatch claims up to limit unprocessed entries and processes them in
// one transaction. If that fails for a reason other than unavailability, the
// same entries are processed one per transaction, and an entry that fails on
// its own is marked failed (design D5a).
func (r *Repository) ProcessBatch(ctx context.Context, limit int) (int, error) {
	ids, err := r.process(ctx, nil, limit)
	if err == nil {
		return len(ids), nil
	}
	if postgres.IsUnavailable(err) || len(ids) == 0 {
		return 0, classify("process batch", err)
	}

	r.log.Error("batch failed, processing its entries one by one", "entries", len(ids), "error", err)
	n := 0
	for _, id := range ids {
		done, err := r.process(ctx, &id, 1)
		switch {
		case err == nil:
			n += len(done)
		case postgres.IsUnavailable(err):
			return n, classify("process entry", err)
		default:
			r.log.Error("entry failed", "inbox_id", id, "error", err)
			if err := r.markFailed(ctx, id, err); err != nil {
				return n, classify("mark entry failed", err)
			}
			n++
		}
	}
	return n, nil
}

// process claims entries (only entry id when given), folds them, applies the
// deltas, marks the entries and commits. It returns the claimed ids, also on
// error, so that the caller can isolate a failing entry.
func (r *Repository) process(ctx context.Context, id *int64, limit int) (ids []int64, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	entries, err := claim(ctx, tx, id, limit)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if len(entries) == 0 {
		return nil, tx.Commit(ctx)
	}

	res := domain.Fold(entries)
	decisions := make(map[int64]domain.Decision, len(res.Decisions))
	for _, d := range res.Decisions {
		decisions[d.EntryID] = d
	}
	for _, d := range res.Deltas { // sorted by channel: rows are locked in the same order by every processor
		if err := upsertRocket(ctx, tx, d); err != nil {
			return ids, err
		}
		if d.Explosion == nil {
			continue
		}
		written, err := writeExplosion(ctx, tx, d.Channel, d.Explosion)
		if err != nil {
			return ids, err
		}
		if !written {
			decisions[d.Explosion.EntryID] = domain.Decision{EntryID: d.Explosion.EntryID, Outcome: domain.Quarantined, Reason: domain.ReasonAlreadyExploded}
		}
	}
	if err := mark(ctx, tx, decisions); err != nil {
		return ids, err
	}
	for _, d := range decisions {
		if d.Outcome == domain.Quarantined {
			r.log.Warn("entry quarantined", "inbox_id", d.EntryID, "reason", d.Reason)
		}
	}
	return ids, tx.Commit(ctx)
}

func claim(ctx context.Context, tx pgx.Tx, id *int64, limit int) ([]domain.InboxEntry, error) {
	rows, err := tx.Query(ctx, `
		select id, channel, message_number, message_time, message_type, body, truncated
		from inbox
		where processed_at is null and ($1::bigint is null or id = $1)
		order by id
		limit $2
		for update skip locked`, id, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.InboxEntry, error) {
		var e domain.InboxEntry
		err := row.Scan(&e.ID, &e.Channel, &e.MessageNumber, &e.MessageTime, &e.MessageType, &e.Body, &e.Truncated)
		return e, err
	})
}

// upsertRocket applies everything of d except the explosion with the rules of
// domain.Rocket.Apply, as one atomic statement: speed by increment, launch
// written once, mission only from a higher number, last applied as maximum.
func upsertRocket(ctx context.Context, tx pgx.Tx, d domain.Delta) error {
	var (
		rocketType, mission *string
		launchSpeed         *int64
		launchedAt          any
		missionNumber       int64
		lastNumber          int64
		lastAt              any
	)
	if d.Launch != nil {
		rocketType, launchSpeed, launchedAt = &d.Launch.RocketType, &d.Launch.Speed, d.Launch.At
	}
	if d.Mission != nil {
		mission, missionNumber = &d.Mission.Mission, d.Mission.Number
	}
	if d.LastApplied != nil {
		lastNumber, lastAt = d.LastApplied.Number, d.LastApplied.At
	}
	_, err := tx.Exec(ctx, `
		insert into rockets as r (channel, speed, rocket_type, launch_speed, launched_at,
		                          mission, mission_message_number,
		                          last_applied_message_number, last_applied_message_at)
		values ($1, $2 + coalesce($4::bigint, 0), $3, $4, $5, $6, $7, $8, $9)
		on conflict (channel) do update set
		  speed        = r.speed + $2 + case when r.rocket_type is null then coalesce($4::bigint, 0) else 0 end,
		  rocket_type  = coalesce(r.rocket_type, $3),
		  launch_speed = coalesce(r.launch_speed, $4),
		  launched_at  = coalesce(r.launched_at, $5),
		  mission      = case when $7 > r.mission_message_number then $6 else r.mission end,
		  mission_message_number = greatest(r.mission_message_number, $7),
		  last_applied_message_at = case when $8 > r.last_applied_message_number then $9 else r.last_applied_message_at end,
		  last_applied_message_number = greatest(r.last_applied_message_number, $8),
		  updated_at   = now()`,
		d.Channel, d.SpeedDelta, rocketType, launchSpeed, launchedAt, mission, missionNumber, lastNumber, lastAt)
	return err
}

// writeExplosion writes the explosion only if the rocket has not exploded
// yet; false means it had, and the entry is to be quarantined.
func writeExplosion(ctx context.Context, tx pgx.Tx, channel string, x *domain.Explosion) (bool, error) {
	tag, err := tx.Exec(ctx, `
		update rockets set
		  explosion_reason = $2,
		  last_applied_message_at = case when $3 > last_applied_message_number then $4 else last_applied_message_at end,
		  last_applied_message_number = greatest(last_applied_message_number, $3),
		  updated_at = now()
		where channel = $1 and explosion_reason is null`,
		channel, x.Reason, x.Number, x.At)
	return tag.RowsAffected() == 1, err
}

func mark(ctx context.Context, tx pgx.Tx, decisions map[int64]domain.Decision) error {
	ids := make([]int64, 0, len(decisions))
	outcomes := make([]string, 0, len(decisions))
	reasons := make([]*string, 0, len(decisions))
	for _, d := range decisions {
		ids = append(ids, d.EntryID)
		outcomes = append(outcomes, string(d.Outcome))
		if d.Reason == "" {
			reasons = append(reasons, nil)
		} else {
			reasons = append(reasons, &d.Reason)
		}
	}
	_, err := tx.Exec(ctx, `
		update inbox set processed_at = now(), outcome = d.outcome, reason = d.reason
		from unnest($1::bigint[], $2::text[], $3::text[]) as d(id, outcome, reason)
		where inbox.id = d.id`, ids, outcomes, reasons)
	return err
}

func (r *Repository) markFailed(ctx context.Context, id int64, cause error) error {
	_, err := r.pool.Exec(ctx, `
		update inbox set processed_at = now(), outcome = 'failed', reason = $2
		where id = $1 and processed_at is null`, id, cause.Error())
	return err
}
