package intake_test

import (
	"context"
	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/testdb"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type row struct {
	channel   *string
	number    *int64
	body      []byte
	truncated bool
}

func rows(t *testing.T, p *pgxpool.Pool) []row {
	t.Helper()
	rs, err := p.Query(context.Background(), `select channel, message_number, body, truncated from inbox order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.channel, &r.number, &r.body, &r.truncated); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func id(c string, n int64) *handler.Identity { return &handler.Identity{Channel: c, MessageNumber: n} }

func withID(c string, n int64) handler.Metadata { return handler.Metadata{Identity: id(c, n)} }

// migratedDB returns the URL of a fresh database with the production schema.
func migratedDB(t *testing.T) string {
	t.Helper()
	url, _ := testdb.Migrated(t)
	return url
}

// stored is a rockets row inserted directly, bypassing processing.
type stored struct {
	channel  string
	speed    int64
	typ      *string
	mission  *string
	launched *time.Time
	reason   *string
}

func insertRocket(t *testing.T, p *pgxpool.Pool, r stored) {
	t.Helper()
	var launchSpeed *int64
	if r.typ != nil {
		launchSpeed = testdb.Ptr(int64(0))
	}
	_, err := p.Exec(context.Background(), `
		insert into rockets (channel, speed, rocket_type, launch_speed, launched_at, mission,
		                     explosion_reason, last_applied_message_number, last_applied_message_at)
		values ($1, $2, $3, $4, $5, $6, $7, 1, $8)`,
		r.channel, r.speed, r.typ, launchSpeed, r.launched, r.mission, r.reason, testdb.MessageTime)
	if err != nil {
		t.Fatalf("insert rocket %s: %v", r.channel, err)
	}
}

func channels(rs []domain.Rocket) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Channel
	}
	return out
}
