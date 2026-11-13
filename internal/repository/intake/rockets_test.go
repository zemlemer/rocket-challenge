package intake_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/repository/intake"
	"rockets/internal/repository/processing"
	"rockets/internal/testdb"
)

func TestRocketsSorting(t *testing.T) {
	_, p := testdb.Migrated(t)
	t1, t2 := testdb.Ptr(testdb.MessageTime), testdb.Ptr(testdb.MessageTime.Add(time.Hour))
	for _, r := range []stored{
		{channel: "a", speed: 100, typ: testdb.Ptr("Soyuz"), mission: testdb.Ptr("Mars"), launched: t2},
		{channel: "B", speed: 300, typ: testdb.Ptr("Falcon-9"), mission: testdb.Ptr("Moon"), launched: t1, reason: testdb.Ptr("BOOM")},
		{channel: "c", speed: 300, reason: testdb.Ptr("BOOM")},                             // exploded, not launched
		{channel: "d", speed: -5, mission: testdb.Ptr("Mars"), reason: testdb.Ptr("BOOM")}, // exploded, mission known
		{channel: "e", speed: 7}, // neither: hidden
		{channel: "f", speed: 100, typ: testdb.Ptr("Soyuz"), mission: testdb.Ptr("Moon"), launched: t1},
	} {
		insertRocket(t, p, r)
	}
	repo := intake.NewRockets(p)

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
	got, err := intake.NewRockets(p).Rockets(context.Background(), handler.RocketQuery{Sort: handler.SortChannel, Order: handler.Asc})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want an empty non-nil list", got, err)
	}
}

// Visibility follows processing: hidden with only speed changes, visible once
// exploded, launch fields filled in when the launch arrives late.
func TestRocketVisibilityThroughProcessing(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	proc := processing.New(p, testdb.Logger())
	repo := intake.NewRockets(p)

	testdb.InsertEntry(t, p, "c1", 2, domain.TypeSpeedIncreased, `{"by":300}`)
	testdb.Drain(t, proc, 10)
	if _, ok, err := repo.Rocket(ctx, "c1"); err != nil || ok {
		t.Fatalf("rocket with only a speed change: ok=%v err=%v, want hidden", ok, err)
	}

	testdb.InsertEntry(t, p, "c1", 5, domain.TypeExploded, `{"reason":"FROZEN_O_RINGS"}`)
	testdb.Drain(t, proc, 10)
	r, ok, err := repo.Rocket(ctx, "c1")
	if err != nil || !ok {
		t.Fatalf("exploded rocket: ok=%v err=%v, want visible", ok, err)
	}
	if r.RocketType != nil || r.LaunchSpeed != nil || r.LaunchedAt != nil || r.Mission != nil || r.Speed != 300 || *r.ExplosionReason != "FROZEN_O_RINGS" {
		t.Fatalf("exploded before launch = %s", testdb.Describe(r))
	}

	testdb.InsertEntry(t, p, "c1", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`)
	testdb.Drain(t, proc, 10)
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
	insertRocket(t, p, stored{channel: "abc-1", typ: testdb.Ptr("A"), mission: testdb.Ptr("M"), launched: testdb.Ptr(testdb.MessageTime)})
	repo := intake.NewRockets(p)
	for channel, want := range map[string]bool{"abc-1": true, "ABC-1": false, "abc": false, "missing": false} {
		if _, ok, err := repo.Rocket(context.Background(), channel); err != nil || ok != want {
			t.Errorf("Rocket(%q) ok=%v err=%v, want %v", channel, ok, err, want)
		}
	}
}

func TestRocketsUnavailable(t *testing.T) {
	_, p := testdb.Migrated(t)
	repo := intake.NewRockets(p)
	p.Close()
	if _, _, err := repo.Rocket(context.Background(), "c"); !errors.Is(err, handler.ErrUnavailable) {
		t.Errorf("Rocket on closed pool: %v, want ErrUnavailable", err)
	}
	if _, err := repo.Rockets(context.Background(), handler.RocketQuery{Sort: handler.SortChannel, Order: handler.Asc}); !errors.Is(err, handler.ErrUnavailable) {
		t.Errorf("Rockets on closed pool: %v, want ErrUnavailable", err)
	}
}

// Every sort field the handler accepts must have an ORDER BY expression;
// otherwise a valid request would fail with 500.
func TestEverySortFieldIsSupported(t *testing.T) {
	_, p := testdb.Migrated(t)
	repo := intake.NewRockets(p)
	for _, f := range handler.SortFields() {
		if _, err := repo.Rockets(context.Background(), handler.RocketQuery{Sort: f, Order: handler.Asc}); err != nil {
			t.Errorf("sort %q: %v", f, err)
		}
	}
}
