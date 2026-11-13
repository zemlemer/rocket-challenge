package domain_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"rockets/internal/domain"
)

// stream generates the messages of a few rockets as the simulator would
// (message 1 is the launch, at most one explosion per rocket) plus some
// non-conforming entries; IDs are unique.
func stream(rng *rand.Rand) []domain.InboxEntry {
	var es []domain.InboxEntry
	id := int64(0)
	next := func() int64 { id++; return id }
	for r := range rng.IntN(4) + 1 {
		ch := fmt.Sprintf("rocket-%d", r)
		n := int64(rng.IntN(25) + 1)
		explodeAt := int64(-1)
		if rng.IntN(2) == 0 {
			explodeAt = rng.Int64N(n) + 1
		}
		for num := int64(1); num <= n; num++ {
			switch {
			case num == 1:
				es = append(es, launched(next(), ch, rng.Int64N(1000), "M1"))
			case num == explodeAt:
				es = append(es, exploded(next(), ch, num, "BOOM"))
			case rng.IntN(10) == 0: // non-conforming
				es = append(es, entry(next(), ch, num, domain.TypeSpeedIncreased, `{"by":-1}`))
			default:
				switch rng.IntN(3) {
				case 0:
					es = append(es, increased(next(), ch, num, rng.Int64N(10_000)))
				case 1:
					es = append(es, decreased(next(), ch, num, rng.Int64N(10_000)))
				default:
					es = append(es, missionChanged(next(), ch, num, fmt.Sprintf("M%d", num)))
				}
			}
		}
	}
	return es
}

func flat(rs map[string]*domain.Rocket) map[string]string {
	out := map[string]string{}
	for ch, r := range rs {
		s := fmt.Sprintf("speed=%d mission#%d last#%d", r.Speed, r.MissionNumber, r.LastAppliedNumber)
		for _, p := range []*string{r.RocketType, r.Mission, r.ExplosionReason} {
			if p != nil {
				s += " " + *p
			} else {
				s += " <nil>"
			}
		}
		if r.LaunchSpeed != nil {
			s += fmt.Sprintf(" launch=%d", *r.LaunchSpeed)
		}
		if r.LastAppliedAt != nil {
			s += " at=" + r.LastAppliedAt.String()
		}
		out[ch] = s
	}
	return out
}

// Any order and any split into batches gives the same state and decisions as
// processing everything at once in order.
func TestPropertyOrderAndBatchingDoNotMatter(t *testing.T) {
	for i := range 1000 {
		rng := rand.New(rand.NewPCG(uint64(i), 7))
		es := stream(rng)
		wantRockets, wantDecisions := process(es)

		shuffled := append([]domain.InboxEntry(nil), es...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		var batches [][]domain.InboxEntry
		for len(shuffled) > 0 {
			k := rng.IntN(len(shuffled)) + 1
			batches = append(batches, shuffled[:k])
			shuffled = shuffled[k:]
		}
		gotRockets, gotDecisions := process(batches...)

		if !reflect.DeepEqual(flat(gotRockets), flat(wantRockets)) || !reflect.DeepEqual(gotDecisions, wantDecisions) {
			t.Fatalf("iteration %d: state differs\n got  %v\n want %v", i, flat(gotRockets), flat(wantRockets))
		}
	}
}
