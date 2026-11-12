package domain_test

import (
	"testing"

	"rockets/internal/domain"
)

func one(es ...domain.InboxEntry) [][]domain.InboxEntry {
	var bs [][]domain.InboxEntry
	for _, e := range es {
		bs = append(bs, []domain.InboxEntry{e})
	}
	return bs
}

func TestDeltasBeforeLaunch(t *testing.T) {
	rs, _ := process(one(increased(1, "c", 2, 300), launched(2, "c", 500, "ARTEMIS"))...)
	r := rs["c"]
	if r.Speed != 800 || r.RocketType == nil || *r.LaunchSpeed != 500 {
		t.Fatalf("got %+v", r)
	}
}

func TestNotLaunchedYet(t *testing.T) {
	rs, _ := process(one(increased(1, "c", 2, 300), decreased(2, "c", 3, 100))...)
	r := rs["c"]
	if r.RocketType != nil || r.LaunchSpeed != nil || r.Speed != 200 {
		t.Fatalf("got %+v", r)
	}
}

func TestNegativeSpeedNotClamped(t *testing.T) {
	rs, _ := process(one(launched(1, "c", 100, "M"), decreased(2, "c", 2, 300))...)
	if rs["c"].Speed != -200 {
		t.Fatalf("speed = %d", rs["c"].Speed)
	}
}

func TestOlderMissionArrivingLateIsIgnored(t *testing.T) {
	rs, _ := process(one(launched(1, "c", 1, "INIT"), missionChanged(2, "c", 7, "MARS"), missionChanged(3, "c", 4, "MOON"))...)
	r := rs["c"]
	if *r.Mission != "MARS" || r.MissionNumber != 7 {
		t.Fatalf("mission = %s (%d)", *r.Mission, r.MissionNumber)
	}
	// Launch processed after a mission change: the launch's own mission is older.
	rs, _ = process(one(missionChanged(1, "d", 3, "LATER"), launched(2, "d", 1, "FIRST"))...)
	if *rs["d"].Mission != "LATER" {
		t.Fatalf("launch overwrote a newer mission: %s", *rs["d"].Mission)
	}
}

func TestExplosionBeforeAnEarlierMessage(t *testing.T) {
	rs, _ := process(one(launched(1, "c", 100, "M"), exploded(2, "c", 9, "BOOM"), increased(3, "c", 8, 50))...)
	r := rs["c"]
	if r.ExplosionReason == nil || *r.ExplosionReason != "BOOM" || r.Speed != 150 {
		t.Fatalf("got %+v", r)
	}
}

func TestSecondExplosionIsQuarantined(t *testing.T) {
	// Same batch: the first by inbox id wins.
	rs, dec := process([]domain.InboxEntry{exploded(5, "c", 9, "SECOND"), exploded(4, "c", 12, "FIRST")})
	if *rs["c"].ExplosionReason != "FIRST" || dec[5].Outcome != domain.Quarantined || dec[5].Reason != domain.ReasonAlreadyExploded || dec[4].Outcome != domain.Applied {
		t.Fatalf("reason=%s decisions=%+v", *rs["c"].ExplosionReason, dec)
	}
	// Separate batches: the later one finds the rocket exploded.
	rs, dec = process(one(exploded(1, "d", 9, "FIRST"), exploded(2, "d", 3, "SECOND"))...)
	if *rs["d"].ExplosionReason != "FIRST" || dec[2].Outcome != domain.Quarantined {
		t.Fatalf("reason=%s decisions=%+v", *rs["d"].ExplosionReason, dec)
	}
}

func TestLastAppliedIgnoresQuarantinedAndOlder(t *testing.T) {
	bad := entry(4, "c", 3, domain.TypeSpeedIncreased, `{"by":-1}`)
	rs, dec := process(one(launched(1, "c", 1, "M"), increased(2, "c", 5, 1), increased(3, "c", 2, 1), bad)...)
	r := rs["c"]
	if dec[4].Outcome != domain.Quarantined || r.LastAppliedNumber != 5 || !r.LastAppliedAt.Equal(t0.Add(5e9)) {
		t.Fatalf("last applied = %d at %v, decisions %+v", r.LastAppliedNumber, r.LastAppliedAt, dec)
	}
	// An explosion counts as applied only when it is written.
	rs, _ = process(one(exploded(1, "e", 7, "A"), exploded(2, "e", 9, "B"))...)
	if rs["e"].LastAppliedNumber != 7 {
		t.Fatalf("rejected explosion moved last applied: %d", rs["e"].LastAppliedNumber)
	}
}

func TestFoldDeltasSortedByChannel(t *testing.T) {
	res := domain.Fold([]domain.InboxEntry{increased(1, "z", 2, 1), increased(2, "a", 2, 1), increased(3, "m", 2, 1)})
	if len(res.Deltas) != 3 || res.Deltas[0].Channel != "a" || res.Deltas[2].Channel != "z" {
		t.Fatalf("deltas not sorted: %+v", res.Deltas)
	}
}
