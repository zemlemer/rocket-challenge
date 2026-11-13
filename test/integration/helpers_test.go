package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/postgres"
	"rockets/internal/repository/intake"
	"rockets/internal/server"
	"rockets/internal/testdb"
)

// limitedPool connects with the given session limits, like the processor.
func limitedPool(t *testing.T, url string, limits postgres.SessionLimits) *pgxpool.Pool {
	t.Helper()
	cfg, err := postgres.PoolConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	postgres.WithSessionLimits(cfg, limits)
	p, err := postgres.ConnectConfig(context.Background(), cfg, 10*time.Second, testdb.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

type message struct {
	channel  string
	number   int64
	typ, pay string
}

// messages generates the messages of n rockets, as the simulator would, plus
// a few non-conforming ones, in shuffled order.
func messages(rng *rand.Rand, rockets, perRocket int) []message {
	var ms []message
	for r := range rockets {
		ch := fmt.Sprintf("rocket-%d", r)
		boom := int64(rng.IntN(perRocket) + 2)
		for num := int64(1); num <= int64(perRocket); num++ {
			switch {
			case num == 1:
				ms = append(ms, message{ch, 1, domain.TypeLaunched, fmt.Sprintf(`{"type":"Falcon-9","launchSpeed":%d,"mission":"M1"}`, rng.IntN(1000))})
			case num == boom:
				ms = append(ms, message{ch, num, domain.TypeExploded, `{"reason":"BOOM"}`})
			case rng.IntN(15) == 0:
				ms = append(ms, message{ch, num, domain.TypeSpeedIncreased, `{"by":-1}`})
			case rng.IntN(5) == 0:
				ms = append(ms, message{ch, num, domain.TypeMissionChanged, fmt.Sprintf(`{"newMission":"M%d"}`, num)})
			case rng.IntN(2) == 0:
				ms = append(ms, message{ch, num, domain.TypeSpeedIncreased, fmt.Sprintf(`{"by":%d}`, rng.IntN(10000))})
			default:
				ms = append(ms, message{ch, num, domain.TypeSpeedDecreased, fmt.Sprintf(`{"by":%d}`, rng.IntN(10000))})
			}
		}
	}
	rng.Shuffle(len(ms), func(a, b int) { ms[a], ms[b] = ms[b], ms[a] })
	return ms
}

func insertAll(t *testing.T, p *pgxpool.Pool, ms []message) {
	t.Helper()
	for _, m := range ms {
		testdb.InsertEntry(t, p, m.channel, m.number, m.typ, m.pay)
	}
}

func unprocessed(t *testing.T, p *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := p.QueryRow(context.Background(), `select count(*) from inbox where processed_at is null`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitProcessed waits until no entry is unprocessed.
func waitProcessed(t *testing.T, p *pgxpool.Pool, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for unprocessed(t, p) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d entries still unprocessed after %s", unprocessed(t, p), within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertMatchesReference compares every rocket with the domain reference
// computed from all inbox entries.
func assertMatchesReference(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	want := testdb.Expected(testdb.AllEntries(t, p))
	for ch, w := range want {
		if got := testdb.LoadRocket(t, p, ch); testdb.Describe(got) != testdb.Describe(w) {
			t.Fatalf("%s:\n got  %s\n want %s", ch, testdb.Describe(got), testdb.Describe(w))
		}
	}
}

// serverRoutes wires intake, the read endpoints and stats over pool, as
// `rockets server` does.
func serverRoutes(p *pgxpool.Pool) http.Handler {
	log := testdb.Logger()
	rockets := &handler.Rockets{Repo: intake.NewRockets(p), Log: log}
	return server.Routes(server.Handlers{
		Messages: &handler.Messages{Repo: intake.NewInbox(p), Log: log},
		Health:   handler.Health{},
		Rockets:  http.HandlerFunc(rockets.List),
		Rocket:   http.HandlerFunc(rockets.Get),
		Stats:    &handler.Stats{Repo: intake.NewStats(p), Log: log},
	})
}

func getRocket(t *testing.T, routes http.Handler, channel string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rockets/"+channel, nil))
	var m map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &m) != nil {
		t.Fatalf("GET /rockets/%s = %d %s", channel, rec.Code, rec.Body)
	}
	return m
}
