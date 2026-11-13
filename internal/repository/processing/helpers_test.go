package processing_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type outcome struct {
	outcome string
	reason  *string
}

func outcomes(t *testing.T, p *pgxpool.Pool) map[int64]outcome {
	t.Helper()
	rs, err := p.Query(context.Background(), `select id, coalesce(outcome, ''), reason from inbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	out := map[int64]outcome{}
	for rs.Next() {
		var id int64
		var o outcome
		if err := rs.Scan(&id, &o.outcome, &o.reason); err != nil {
			t.Fatal(err)
		}
		out[id] = o
	}
	return out
}
