package domain

import (
	"cmp"
	"slices"
	"time"
)

// Outcome of processing one inbox entry.
type Outcome string

const (
	Applied     Outcome = "applied"
	Quarantined Outcome = "quarantined" // the sender sent something invalid
	Failed      Outcome = "failed"      // valid, but the system could not apply it
)

// ReasonAlreadyExploded is the quarantine reason for a second explosion.
const ReasonAlreadyExploded = "rocket already exploded"

// Decision is what happens to one inbox entry.
type Decision struct {
	EntryID int64
	Outcome Outcome
	Reason  string
}

// Stamp is a message number with its message time.
type Stamp struct {
	Number int64
	At     time.Time
}

// Launch holds the launch attributes (always from message number 1).
type Launch struct {
	RocketType string
	Speed      int64
	At         time.Time
}

// MissionValue is a mission with the number of the message it came from.
type MissionValue struct {
	Number  int64
	Mission string
}

// Explosion is the first explosion seen for a rocket in a batch. Whether it
// is applied is decided when it is written: only if the rocket has not
// exploded yet; otherwise EntryID is quarantined.
type Explosion struct {
	EntryID int64
	Stamp
	Reason string
}

// Delta is the combined, order-independent effect of a set of conforming
// messages of one rocket.
type Delta struct {
	Channel     string
	SpeedDelta  int64 // increases minus decreases; launch speed is separate
	Launch      *Launch
	Mission     *MissionValue // highest-numbered mission
	Explosion   *Explosion
	LastApplied *Stamp // highest-numbered applied message, explosion excluded
}

// BatchResult is the outcome of folding a batch: a decision per entry and
// one delta per rocket, sorted by channel (the order rows are locked in).
type BatchResult struct {
	Decisions []Decision
	Deltas    []Delta
}

// Fold classifies entries and combines the conforming ones into one Delta
// per rocket. Entries are taken in inbox order (by ID), which decides which
// of two explosions in the same batch is kept.
func Fold(entries []InboxEntry) BatchResult {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b InboxEntry) int { return cmp.Compare(a.ID, b.ID) })

	var res BatchResult
	deltas := map[string]*Delta{}
	for _, e := range sorted {
		m, err := Classify(e)
		if err != nil {
			res.Decisions = append(res.Decisions, Decision{e.ID, Quarantined, err.Error()})
			continue
		}
		d := deltas[m.Channel]
		if d == nil {
			d = &Delta{Channel: m.Channel}
			deltas[m.Channel] = d
		}
		if m.Type == TypeExploded && d.Explosion != nil {
			res.Decisions = append(res.Decisions, Decision{e.ID, Quarantined, ReasonAlreadyExploded})
			continue
		}
		d.add(e.ID, m)
		res.Decisions = append(res.Decisions, Decision{e.ID, Applied, ""})
	}
	for _, d := range deltas {
		res.Deltas = append(res.Deltas, *d)
	}
	slices.SortFunc(res.Deltas, func(a, b Delta) int { return cmp.Compare(a.Channel, b.Channel) })
	return res
}

func (d *Delta) add(entryID int64, m Message) {
	stamp := Stamp{m.Number, m.Time}
	switch m.Type {
	case TypeLaunched:
		d.Launch = &Launch{RocketType: m.RocketType, Speed: m.LaunchSpeed, At: m.Time}
		d.Mission = higherMission(d.Mission, &MissionValue{m.Number, m.Mission})
	case TypeSpeedIncreased:
		d.SpeedDelta += m.By
	case TypeSpeedDecreased:
		d.SpeedDelta -= m.By
	case TypeMissionChanged:
		d.Mission = higherMission(d.Mission, &MissionValue{m.Number, m.Mission})
	case TypeExploded:
		d.Explosion = &Explosion{EntryID: entryID, Stamp: stamp, Reason: m.Reason}
		return // its stamp counts only if the explosion is written
	}
	d.LastApplied = laterStamp(d.LastApplied, &stamp)
}

// Rocket is the stored state of a rocket. Apply is the reference for the
// SQL upsert in the repository: both must give the same result.
type Rocket struct {
	Channel           string
	Speed             int64
	RocketType        *string
	LaunchSpeed       *int64
	LaunchedAt        *time.Time
	Mission           *string
	MissionNumber     int64
	ExplosionReason   *string
	LastAppliedNumber int64
	LastAppliedAt     *time.Time
}

// Apply writes d into r with the order-independent rules and reports whether
// d's explosion was written (false: the rocket had already exploded).
func (r *Rocket) Apply(d Delta) (explosionWritten bool) {
	r.Speed += d.SpeedDelta
	if d.Launch != nil && r.RocketType == nil {
		r.Speed += d.Launch.Speed
		r.RocketType, r.LaunchSpeed, r.LaunchedAt = &d.Launch.RocketType, &d.Launch.Speed, &d.Launch.At
	}
	if d.Mission != nil && d.Mission.Number > r.MissionNumber {
		r.Mission, r.MissionNumber = &d.Mission.Mission, d.Mission.Number
	}
	last := d.LastApplied
	if d.Explosion != nil && r.ExplosionReason == nil {
		r.ExplosionReason = &d.Explosion.Reason
		last = laterStamp(last, &d.Explosion.Stamp)
		explosionWritten = true
	}
	if last != nil && last.Number > r.LastAppliedNumber {
		r.LastAppliedNumber, r.LastAppliedAt = last.Number, &last.At
	}
	return explosionWritten
}
