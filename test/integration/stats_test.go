package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/repository/processing"
	"rockets/internal/testdb"
)

type statsReport struct {
	Inbox struct {
		Received, Pending, Applied, Quarantined, Failed int64
		OldestPendingAt                                 *string
	}
	Rockets []struct {
		Channel                                     string
		HighestMessageNumber                        int64
		Received, Missing, Pending, Applied, Failed int64
		Quarantined                                 int64
		Complete                                    bool
	}
}

func getStats(t *testing.T, routes http.Handler) statsReport {
	t.Helper()
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	var r statsReport
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &r) != nil {
		t.Fatalf("GET /stats = %d %s", rec.Code, rec.Body)
	}
	return r
}

// count runs a counting query against the inbox, the oracle for /stats.
func count(t *testing.T, p *pgxpool.Pool, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := p.QueryRow(context.Background(), `select count(*) from inbox where `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Messages posted through intake, with a redelivery, a gap, an invalid body
// and an invalid channel, are reported as the inbox holds them before and
// after processing.
func TestStatsReportsIntakeAndProcessing(t *testing.T) {
	_, p := testdb.Migrated(t)
	routes := serverRoutes(p)
	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body)))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST %s: %d", body, rec.Code)
		}
	}
	ms := messages(rand.New(rand.NewPCG(7, 7)), 3, 40)
	ms = append(ms, ms[0]) // redelivery
	for _, m := range ms {
		if m.channel == "rocket-2" && m.number == 17 {
			continue // gap
		}
		post(fmt.Sprintf(`{"metadata":{"channel":%q,"messageNumber":%d,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":%q},"message":%s}`,
			m.channel, m.number, m.typ, m.pay))
	}
	post(`not json`)
	post(`{"metadata":{"channel":"bad channel","messageNumber":1,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},"message":{}}`)

	before := getStats(t, routes)
	if before.Inbox.Received != 3*40-1+2 || before.Inbox.Pending != before.Inbox.Received || before.Inbox.OldestPendingAt == nil {
		t.Fatalf("before processing: %+v", before.Inbox)
	}

	proc := processing.New(p, testdb.Logger())
	for {
		n, err := proc.ProcessBatch(context.Background(), 25, domain.Fold)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}

	after := getStats(t, routes)
	in := after.Inbox
	if in.Received != count(t, p, "true") || in.Pending != 0 || in.OldestPendingAt != nil ||
		in.Applied != count(t, p, "outcome = 'applied'") || in.Quarantined != count(t, p, "outcome = 'quarantined'") ||
		in.Failed != 0 || in.Received != in.Applied+in.Quarantined {
		t.Fatalf("after processing: %+v", in)
	}
	if len(after.Rockets) != 3 {
		t.Fatalf("rockets = %+v, want rocket-0..2 without the invalid channel", after.Rockets)
	}
	for i, r := range after.Rockets {
		ch := fmt.Sprintf("rocket-%d", i)
		wantMissing := int64(0)
		if ch == "rocket-2" {
			wantMissing = 1
		}
		if r.Channel != ch || r.HighestMessageNumber != 40 || r.Missing != wantMissing || r.Complete != (wantMissing == 0) ||
			r.Received != count(t, p, "channel = $1", ch) || r.Applied != count(t, p, "channel = $1 and outcome = 'applied'", ch) {
			t.Errorf("rocket %d = %+v", i, r)
		}
	}
}
