package processing_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"rockets/internal/domain"
	"rockets/internal/repository/processing"
	"rockets/internal/testdb"
)

func TestProcessBatchAppliesAndMarks(t *testing.T) {
	_, p := testdb.Migrated(t)
	launch := testdb.InsertEntry(t, p, "c", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`)
	inc := testdb.InsertEntry(t, p, "c", 2, domain.TypeSpeedIncreased, `{"by":3000}`)
	bad := testdb.InsertEntry(t, p, "c", 3, domain.TypeSpeedDecreased, `{"by":-5}`)
	if n := testdb.Drain(t, processing.New(p, testdb.Logger()), 1000); n != 3 {
		t.Fatalf("processed %d, want 3", n)
	}
	r := testdb.LoadRocket(t, p, "c")
	if r.Speed != 3500 || *r.RocketType != "Falcon-9" || *r.Mission != "ARTEMIS" || r.LastAppliedNumber != 2 {
		t.Fatalf("rocket = %+v", r)
	}
	o := outcomes(t, p)
	if o[launch].outcome != "applied" || o[inc].outcome != "applied" || o[bad].outcome != "quarantined" || !strings.Contains(*o[bad].reason, "message.by") {
		t.Fatalf("outcomes = %+v", o)
	}
	var processedAt int
	_ = p.QueryRow(context.Background(), `select count(*) from inbox where processed_at is not null`).Scan(&processedAt)
	if processedAt != 3 {
		t.Fatalf("%d entries have processed_at", processedAt)
	}
}

func TestClaimSkipsProcessedAndLocked(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	repo := processing.New(p, testdb.Logger())
	first := testdb.InsertEntry(t, p, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":1,"mission":"M"}`)
	testdb.Drain(t, repo, 10)
	locked := testdb.InsertEntry(t, p, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	free := testdb.InsertEntry(t, p, "c", 3, domain.TypeSpeedIncreased, `{"by":1}`)

	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select 1 from inbox where id = $1 for update`, locked); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ProcessBatch(ctx, 10, domain.Fold); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want only the free entry", n, err)
	}
	o := outcomes(t, p)
	if o[first].outcome != "applied" || o[free].outcome != "applied" || o[locked].outcome != "" {
		t.Fatalf("outcomes = %+v", o)
	}
}

func TestSecondExplosionAcrossBatchesQuarantined(t *testing.T) {
	_, p := testdb.Migrated(t)
	repo := processing.New(p, testdb.Logger())
	testdb.InsertEntry(t, p, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":1,"mission":"M"}`)
	testdb.InsertEntry(t, p, "c", 9, domain.TypeExploded, `{"reason":"FIRST"}`)
	testdb.Drain(t, repo, 10)
	second := testdb.InsertEntry(t, p, "c", 4, domain.TypeExploded, `{"reason":"SECOND"}`)
	testdb.Drain(t, repo, 10)
	r := testdb.LoadRocket(t, p, "c")
	o := outcomes(t, p)
	if *r.ExplosionReason != "FIRST" || r.LastAppliedNumber != 9 || o[second].outcome != "quarantined" || *o[second].reason != domain.ReasonAlreadyExploded {
		t.Fatalf("rocket=%+v outcome=%+v", r, o[second])
	}
}

// A statement error on one entry (forced by a test-only constraint) marks
// exactly that entry failed; the rest of the batch is applied.
func TestPoisonEntryIsIsolatedAsFailed(t *testing.T) {
	_, p := testdb.Migrated(t)
	if _, err := p.Exec(context.Background(), `alter table rockets add constraint test_no_poison check (mission is distinct from 'POISON')`); err != nil {
		t.Fatal(err)
	}
	testdb.InsertEntry(t, p, "a", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":100,"mission":"M"}`)
	poison := testdb.InsertEntry(t, p, "b", 7, domain.TypeMissionChanged, `{"newMission":"POISON"}`)
	testdb.InsertEntry(t, p, "a", 2, domain.TypeSpeedIncreased, `{"by":5}`)
	testdb.InsertEntry(t, p, "c", 3, domain.TypeSpeedIncreased, `{"by":9}`)

	if n := testdb.Drain(t, processing.New(p, testdb.Logger()), 100); n != 4 {
		t.Fatalf("processed %d, want 4", n)
	}
	o := outcomes(t, p)
	failed := 0
	for id, x := range o {
		if x.outcome == "failed" {
			failed++
			if id != poison || !strings.Contains(*x.reason, "test_no_poison") {
				t.Fatalf("wrong entry failed: %d %+v", id, x)
			}
		}
	}
	if failed != 1 || testdb.LoadRocket(t, p, "a").Speed != 105 || testdb.LoadRocket(t, p, "c").Speed != 9 {
		t.Fatalf("failed=%d a=%+v", failed, testdb.LoadRocket(t, p, "a"))
	}
}

// The SQL upsert gives the same state as the domain reference for random
// streams, random insertion order and random batch sizes.
func TestUpsertMatchesDomainReference(t *testing.T) {
	_, p := testdb.Migrated(t)
	for i := range 30 {
		rng := rand.New(rand.NewPCG(uint64(i), 3))
		if _, err := p.Exec(context.Background(), `truncate inbox, rockets`); err != nil {
			t.Fatal(err)
		}
		type msg struct {
			ch       string
			num      int64
			typ, pay string
		}
		var msgs []msg
		for r := range rng.IntN(3) + 1 {
			ch := fmt.Sprintf("r%d", r)
			n := int64(rng.IntN(20) + 1)
			boom := int64(-1)
			if rng.IntN(2) == 0 {
				boom = rng.Int64N(n) + 1
			}
			for num := int64(1); num <= n; num++ {
				switch {
				case num == 1:
					msgs = append(msgs, msg{ch, 1, domain.TypeLaunched, fmt.Sprintf(`{"type":"T","launchSpeed":%d,"mission":"M1"}`, rng.IntN(999))})
				case num == boom:
					msgs = append(msgs, msg{ch, num, domain.TypeExploded, `{"reason":"BOOM"}`})
				case rng.IntN(8) == 0:
					msgs = append(msgs, msg{ch, num, domain.TypeSpeedIncreased, `{"by":-1}`})
				case rng.IntN(3) == 0:
					msgs = append(msgs, msg{ch, num, domain.TypeMissionChanged, fmt.Sprintf(`{"newMission":"M%d"}`, num)})
				case rng.IntN(2) == 0:
					msgs = append(msgs, msg{ch, num, domain.TypeSpeedIncreased, fmt.Sprintf(`{"by":%d}`, rng.IntN(9999))})
				default:
					msgs = append(msgs, msg{ch, num, domain.TypeSpeedDecreased, fmt.Sprintf(`{"by":%d}`, rng.IntN(9999))})
				}
			}
		}
		rng.Shuffle(len(msgs), func(a, b int) { msgs[a], msgs[b] = msgs[b], msgs[a] })
		for _, m := range msgs {
			testdb.InsertEntry(t, p, m.ch, m.num, m.typ, m.pay)
		}

		want := testdb.Expected(testdb.AllEntries(t, p))

		testdb.Drain(t, processing.New(p, testdb.Logger()), rng.IntN(7)+1)
		for ch, w := range want {
			got := testdb.LoadRocket(t, p, ch)
			if testdb.Describe(got) != testdb.Describe(w) {
				t.Fatalf("iteration %d, %s:\n got  %s\n want %s", i, ch, testdb.Describe(got), testdb.Describe(w))
			}
		}
	}
}
