package testdb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
)

// MessageTime is the base message time of entries inserted by InsertEntry;
// message n gets MessageTime + n seconds.
var MessageTime = time.Date(2022, 2, 2, 18, 39, 5, 0, time.UTC)

// InsertEntry stores an inbox entry with full metadata, as intake would, and
// returns its id.
func InsertEntry(t testing.TB, p *pgxpool.Pool, channel string, number int64, typ, payload string) int64 {
	t.Helper()
	var id int64
	body := `{"metadata":{"channel":"` + channel + `"},"message":` + payload + `}`
	err := p.QueryRow(context.Background(), `
		insert into inbox (channel, message_number, message_time, message_type, body, truncated)
		values ($1, $2, $3, $4, $5, false) returning id`,
		channel, number, MessageTime.Add(time.Duration(number)*time.Second), typ, []byte(body)).Scan(&id)
	if err != nil {
		t.Fatalf("testdb: insert entry: %v", err)
	}
	return id
}

// LoadRocket reads a rocket's stored state.
func LoadRocket(t testing.TB, p *pgxpool.Pool, channel string) domain.Rocket {
	t.Helper()
	r := domain.Rocket{Channel: channel}
	err := p.QueryRow(context.Background(), `
		select speed, rocket_type, launch_speed, launched_at, mission, mission_message_number,
		       explosion_reason, last_applied_message_number, last_applied_message_at
		from rockets where channel = $1`, channel).Scan(&r.Speed, &r.RocketType, &r.LaunchSpeed, &r.LaunchedAt,
		&r.Mission, &r.MissionNumber, &r.ExplosionReason, &r.LastAppliedNumber, &r.LastAppliedAt)
	if err != nil {
		t.Fatalf("testdb: load rocket %s: %v", channel, err)
	}
	return r
}

// AllEntries reads every inbox entry as the processor sees it.
func AllEntries(t testing.TB, p *pgxpool.Pool) []domain.InboxEntry {
	t.Helper()
	rows, err := p.Query(context.Background(), `select id, channel, message_number, message_time, message_type, body, truncated from inbox order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var all []domain.InboxEntry
	for rows.Next() {
		var e domain.InboxEntry
		if err := rows.Scan(&e.ID, &e.Channel, &e.MessageNumber, &e.MessageTime, &e.MessageType, &e.Body, &e.Truncated); err != nil {
			t.Fatal(err)
		}
		all = append(all, e)
	}
	return all
}

// Expected computes the rockets the domain derives from entries processed at
// once — the reference that processed state is compared with.
func Expected(entries []domain.InboxEntry) map[string]domain.Rocket {
	want := map[string]domain.Rocket{}
	for _, d := range domain.Fold(entries).Deltas {
		r := domain.Rocket{Channel: d.Channel}
		r.Apply(d)
		want[d.Channel] = r
	}
	return want
}

// Describe renders a rocket as one comparable line.
func Describe(r domain.Rocket) string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	ls := "<nil>"
	if r.LaunchSpeed != nil {
		ls = fmt.Sprint(*r.LaunchSpeed)
	}
	ts := func(p *time.Time) string {
		if p == nil {
			return "<nil>"
		}
		return p.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("speed=%d type=%s launch=%s at=%s mission=%s#%d boom=%s last=%d@%s",
		r.Speed, s(r.RocketType), ls, ts(r.LaunchedAt), s(r.Mission), r.MissionNumber, s(r.ExplosionReason), r.LastAppliedNumber, ts(r.LastAppliedAt))
}
