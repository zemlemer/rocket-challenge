package repository_test

import (
	"context"
	"rockets/internal/handler"
	"rockets/internal/testdb"
	"testing"

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
