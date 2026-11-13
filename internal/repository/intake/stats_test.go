package intake_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/repository/intake"
	"rockets/internal/testdb"
)

// mark sets the outcome of an entry as processing would.
func mark(t *testing.T, p *pgxpool.Pool, id int64, outcome string) {
	t.Helper()
	if _, err := p.Exec(context.Background(),
		`update inbox set processed_at = now(), outcome = $2 where id = $1`, id, outcome); err != nil {
		t.Fatal(err)
	}
}

// insertAnonymous stores an entry without identity, as intake does for a
// body whose metadata could not be parsed.
func insertAnonymous(t *testing.T, p *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := p.QueryRow(context.Background(),
		`insert into inbox (body, truncated) values ('not json', false) returning id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func byChannel(cs []handler.ChannelStats) map[string]handler.ChannelStats {
	m := map[string]handler.ChannelStats{}
	for _, c := range cs {
		m[c.Channel] = c
	}
	return m
}

func TestStatsEmpty(t *testing.T) {
	_, p := testdb.Migrated(t)
	s, err := intake.NewStats(p).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Inbox != (handler.InboxTotals{}) || len(s.Channels) != 0 || s.AsOf.IsZero() {
		t.Fatalf("empty inbox: %+v", s)
	}
}

func TestStatsCounts(t *testing.T) {
	_, p := testdb.Migrated(t)
	ins := func(c string, n int64) int64 {
		return testdb.InsertEntry(t, p, c, n, domain.TypeSpeedIncreased, `{"by":1}`)
	}

	// c1: 1, 2, 5 (gap of two); 1 applied, 2 quarantined, 5 pending.
	mark(t, p, ins("c1", 1), "applied")
	mark(t, p, ins("c1", 2), "quarantined")
	ins("c1", 5)
	// c2: 1 failed, 0 quarantined (not counted per channel).
	mark(t, p, ins("c2", 1), "failed")
	mark(t, p, ins("c2", 0), "quarantined")
	// c3: only a number below 1, so no channel row.
	mark(t, p, ins("c3", -4), "quarantined")
	// Without identity: pending.
	insertAnonymous(t, p)

	s, err := intake.NewStats(p).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := handler.InboxTotals{Received: 7, Pending: 2, Applied: 1, Quarantined: 3, Failed: 1}
	got := s.Inbox
	got.OldestPendingAt = nil
	if got != want {
		t.Errorf("totals = %+v, want %+v", got, want)
	}
	if s.Inbox.OldestPendingAt == nil {
		t.Error("oldest pending missing")
	}

	cs := byChannel(s.Channels)
	if len(cs) != 2 {
		t.Fatalf("channels = %+v, want c1 and c2", s.Channels)
	}
	if c := cs["c1"]; c != (handler.ChannelStats{Channel: "c1", Highest: 5, Received: 3, Pending: 1, Applied: 1, Quarantined: 1}) {
		t.Errorf("c1 = %+v", c)
	}
	if c := cs["c2"]; c != (handler.ChannelStats{Channel: "c2", Highest: 1, Received: 1, Failed: 1}) {
		t.Errorf("c2 = %+v", c)
	}
}

// A redelivered message ends at intake's unique index, so it is counted once.
func TestStatsRedeliveryCountedOnce(t *testing.T) {
	_, p := testdb.Migrated(t)
	inbox := intake.NewInbox(p)
	for _, n := range []int64{1, 2, 2, 3} {
		if err := inbox.Save(context.Background(), handler.InboxEntry{Metadata: withID("c1", n), Body: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := intake.NewStats(p).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Inbox.Received != 3 || !slices.Equal(s.Channels, []handler.ChannelStats{{Channel: "c1", Highest: 3, Received: 3, Pending: 3}}) {
		t.Fatalf("stats = %+v", s)
	}
}

func TestStatsOldestPending(t *testing.T) {
	_, p := testdb.Migrated(t)
	old := testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{}`)
	mark(t, p, testdb.InsertEntry(t, p, "c1", 2, domain.TypeSpeedIncreased, `{}`), "applied")
	testdb.InsertEntry(t, p, "c1", 3, domain.TypeSpeedIncreased, `{}`)
	if _, err := p.Exec(context.Background(),
		`update inbox set received_at = now() - interval '2.5 seconds' where id = $1`, old); err != nil {
		t.Fatal(err)
	}

	s, err := intake.NewStats(p).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Inbox.OldestPendingAt == nil {
		t.Fatal("oldest pending missing")
	}
	if age := s.AsOf.Sub(*s.Inbox.OldestPendingAt); age < 2500*time.Millisecond || age > 3500*time.Millisecond {
		t.Errorf("oldest pending age = %v, want about 2.5 s", age)
	}
}

// The report reads past rows a processor has locked instead of waiting.
func TestStatsDoesNotWaitForLockedRows(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{}`)

	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select id from inbox where processed_at is null for update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update inbox set processed_at = now(), outcome = 'applied'`); err != nil {
		t.Fatal(err)
	}

	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s, err := intake.NewStats(p).Stats(sctx)
	if err != nil {
		t.Fatalf("stats while rows are locked: %v", err)
	}
	if s.Inbox.Pending != 1 || s.Inbox.Applied != 0 {
		t.Errorf("uncommitted processing visible: %+v", s.Inbox)
	}
}

func TestStatsUnavailable(t *testing.T) {
	_, p := testdb.Migrated(t)
	repo := intake.NewStats(p)
	p.Close()
	_, err := repo.Stats(context.Background())
	if !errors.Is(err, handler.ErrUnavailable) || !strings.Contains(err.Error(), "read stats") {
		t.Errorf("Stats on closed pool: %v, want ErrUnavailable", err)
	}
}
