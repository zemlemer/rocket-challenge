package domain_test

import (
	"strconv"
	"time"

	"rockets/internal/domain"
)

var t0 = time.Date(2022, 2, 2, 18, 39, 5, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// entry builds an inbox entry with full metadata and the given payload.
func entry(id int64, channel string, number int64, typ, payload string) domain.InboxEntry {
	return domain.InboxEntry{
		ID:            id,
		Channel:       ptr(channel),
		MessageNumber: ptr(number),
		MessageTime:   ptr(t0.Add(time.Duration(number) * time.Second)),
		MessageType:   ptr(typ),
		Body:          []byte(`{"metadata":{},"message":` + payload + `}`),
	}
}

func launched(id int64, ch string, speed int64, mission string) domain.InboxEntry {
	return entry(id, ch, 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":`+itoa(speed)+`,"mission":"`+mission+`"}`)
}
func increased(id int64, ch string, n, by int64) domain.InboxEntry {
	return entry(id, ch, n, domain.TypeSpeedIncreased, `{"by":`+itoa(by)+`}`)
}
func decreased(id int64, ch string, n, by int64) domain.InboxEntry {
	return entry(id, ch, n, domain.TypeSpeedDecreased, `{"by":`+itoa(by)+`}`)
}
func missionChanged(id int64, ch string, n int64, mission string) domain.InboxEntry {
	return entry(id, ch, n, domain.TypeMissionChanged, `{"newMission":"`+mission+`"}`)
}
func exploded(id int64, ch string, n int64, reason string) domain.InboxEntry {
	return entry(id, ch, n, domain.TypeExploded, `{"reason":"`+reason+`"}`)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// process folds each batch and applies its deltas to the rockets, like the
// repository does, and returns the rockets and all decisions.
func process(batches ...[]domain.InboxEntry) (map[string]*domain.Rocket, map[int64]domain.Decision) {
	rockets := map[string]*domain.Rocket{}
	decisions := map[int64]domain.Decision{}
	for _, b := range batches {
		res := domain.Fold(b)
		for _, dec := range res.Decisions {
			decisions[dec.EntryID] = dec
		}
		for _, d := range res.Deltas {
			r := rockets[d.Channel]
			if r == nil {
				r = &domain.Rocket{Channel: d.Channel}
				rockets[d.Channel] = r
			}
			if d.Explosion != nil && !r.Apply(d) {
				decisions[d.Explosion.EntryID] = domain.Decision{EntryID: d.Explosion.EntryID, Outcome: domain.Quarantined, Reason: domain.ReasonAlreadyExploded}
			} else if d.Explosion == nil {
				r.Apply(d)
			}
		}
	}
	return rockets, decisions
}
