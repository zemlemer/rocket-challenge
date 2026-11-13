package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/repository"
	"rockets/internal/testdb"
)

// stored is a rockets row inserted directly, bypassing processing.
type stored struct {
	channel  string
	speed    int64
	typ      *string
	mission  *string
	launched *time.Time
	reason   *string
}

func ptr[T any](v T) *T { return &v }

func insertRocket(t *testing.T, p *pgxpool.Pool, r stored) {
	t.Helper()
	var launchSpeed *int64
	if r.typ != nil {
		launchSpeed = ptr(int64(0))
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

func TestRocketsSorting(t *testing.T) {
	_, p := testdb.Migrated(t)
	t1, t2 := ptr(testdb.MessageTime), ptr(testdb.MessageTime.Add(time.Hour))
	for _, r := range []stored{
		{channel: "a", speed: 100, typ: ptr("Soyuz"), mission: ptr("Mars"), launched: t2},
		{channel: "B", speed: 300, typ: ptr("Falcon-9"), mission: ptr("Moon"), launched: t1, reason: ptr("BOOM")},
		{channel: "c", speed: 300, reason: ptr("BOOM")},                      // exploded, not launched
		{channel: "d", speed: -5, mission: ptr("Mars"), reason: ptr("BOOM")}, // exploded, mission known
		{channel: "e", speed: 7}, // neither: hidden
		{channel: "f", speed: 100, typ: ptr("Soyuz"), mission: ptr("Moon"), launched: t1},
	} {
		insertRocket(t, p, r)
	}
	repo := repository.NewRockets(p)

	for _, tc := range []struct {
		sort      handler.SortField
		asc, desc []string
	}{
		{handler.SortChannel, []string{"B", "a", "c", "d", "f"}, []string{"f", "d", "c", "a", "B"}},
		{handler.SortType, []string{"B", "a", "f", "c", "d"}, []string{"a", "f", "B", "c", "d"}},
		{handler.SortMission, []string{"a", "d", "B", "f", "c"}, []string{"B", "f", "a", "d", "c"}},
		{handler.SortSpeed, []string{"d", "a", "f", "B", "c"}, []string{"B", "c", "a", "f", "d"}},
		{handler.SortStatus, []string{"a", "f", "B", "c", "d"}, []string{"B", "c", "d", "a", "f"}},
		{handler.SortLaunchedAt, []string{"B", "f", "a", "c", "d"}, []string{"a", "B", "f", "c", "d"}},
	} {
		for order, want := range map[handler.SortOrder][]string{handler.Asc: tc.asc, handler.Desc: tc.desc} {
			got, err := repo.Rockets(context.Background(), handler.RocketQuery{Sort: tc.sort, Order: order})
			if err != nil {
				t.Fatalf("%s %s: %v", tc.sort, order, err)
			}
			if !slices.Equal(channels(got), want) {
				t.Errorf("%s %s = %v, want %v", tc.sort, order, channels(got), want)
			}
		}
	}

	if _, err := repo.Rockets(context.Background(), handler.RocketQuery{Sort: "altitude", Order: handler.Asc}); err == nil {
		t.Error("unknown sort field accepted")
	}
}

func TestRocketsEmpty(t *testing.T) {
	_, p := testdb.Migrated(t)
	got, err := repository.NewRockets(p).Rockets(context.Background(), handler.RocketQuery{Sort: handler.SortChannel, Order: handler.Asc})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want an empty non-nil list", got, err)
	}
}

// Visibility follows processing: hidden with only speed changes, visible once
// exploded, launch fields filled in when the launch arrives late.
func TestRocketVisibilityThroughProcessing(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	proc := repository.NewProcessing(p, testdb.Logger())
	repo := repository.NewRockets(p)

	testdb.InsertEntry(t, p, "c1", 2, domain.TypeSpeedIncreased, `{"by":300}`)
	drain(t, proc, 10)
	if _, ok, err := repo.Rocket(ctx, "c1"); err != nil || ok {
		t.Fatalf("rocket with only a speed change: ok=%v err=%v, want hidden", ok, err)
	}

	testdb.InsertEntry(t, p, "c1", 5, domain.TypeExploded, `{"reason":"FROZEN_O_RINGS"}`)
	drain(t, proc, 10)
	r, ok, err := repo.Rocket(ctx, "c1")
	if err != nil || !ok {
		t.Fatalf("exploded rocket: ok=%v err=%v, want visible", ok, err)
	}
	if r.RocketType != nil || r.LaunchSpeed != nil || r.LaunchedAt != nil || r.Mission != nil || r.Speed != 300 || *r.ExplosionReason != "FROZEN_O_RINGS" {
		t.Fatalf("exploded before launch = %s", testdb.Describe(r))
	}

	testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`)
	drain(t, proc, 10)
	r, ok, err = repo.Rocket(ctx, "c1")
	if err != nil || !ok || *r.RocketType != "Falcon-9" || *r.LaunchSpeed != 500 || *r.Mission != "ARTEMIS" || r.Speed != 800 || *r.ExplosionReason != "FROZEN_O_RINGS" {
		t.Fatalf("after late launch: ok=%v err=%v rocket=%s", ok, err, testdb.Describe(r))
	}

	list, err := repo.Rockets(ctx, handler.RocketQuery{Sort: handler.SortChannel, Order: handler.Asc})
	if err != nil || !slices.Equal(channels(list), []string{"c1"}) {
		t.Fatalf("list = %v, %v", channels(list), err)
	}
}

func TestRocketExactChannel(t *testing.T) {
	_, p := testdb.Migrated(t)
	insertRocket(t, p, stored{channel: "abc-1", typ: ptr("A"), mission: ptr("M"), launched: ptr(testdb.MessageTime)})
	repo := repository.NewRockets(p)
	for channel, want := range map[string]bool{"abc-1": true, "ABC-1": false, "abc": false, "missing": false} {
		if _, ok, err := repo.Rocket(context.Background(), channel); err != nil || ok != want {
			t.Errorf("Rocket(%q) ok=%v err=%v, want %v", channel, ok, err, want)
		}
	}
}

func TestRocketsUnavailable(t *testing.T) {
	_, p := testdb.Migrated(t)
	repo := repository.NewRockets(p)
	p.Close()
	if _, _, err := repo.Rocket(context.Background(), "c"); !errors.Is(err, handler.ErrUnavailable) {
		t.Errorf("Rocket on closed pool: %v, want ErrUnavailable", err)
	}
	if _, err := repo.Rockets(context.Background(), handler.RocketQuery{Sort: handler.SortChannel, Order: handler.Asc}); !errors.Is(err, handler.ErrUnavailable) {
		t.Errorf("Rockets on closed pool: %v, want ErrUnavailable", err)
	}
}
