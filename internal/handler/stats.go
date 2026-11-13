package handler

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"time"

	"rockets/internal/domain"
	"rockets/pkg/helpers"
)

// Stats handles GET /stats: intake and processing progress and per-rocket
// message completeness, computed from storage (design D1).
type Stats struct {
	Repo StatsRepository
	Log  *slog.Logger
}

type statsJSON struct {
	AsOf    jsonTime          `json:"asOf"`
	Inbox   inboxStatsJSON    `json:"inbox"`
	Rockets []rocketStatsJSON `json:"rockets"`
}

type inboxStatsJSON struct {
	Received                int64     `json:"received"`
	Pending                 int64     `json:"pending"`
	Applied                 int64     `json:"applied"`
	Quarantined             int64     `json:"quarantined"`
	Failed                  int64     `json:"failed"`
	OldestPendingAt         *jsonTime `json:"oldestPendingAt"`
	OldestPendingAgeSeconds float64   `json:"oldestPendingAgeSeconds"`
}

type rocketStatsJSON struct {
	Channel              string `json:"channel"`
	HighestMessageNumber int64  `json:"highestMessageNumber"`
	Received             int64  `json:"received"`
	Missing              int64  `json:"missing"`
	Pending              int64  `json:"pending"`
	Applied              int64  `json:"applied"`
	Quarantined          int64  `json:"quarantined"`
	Failed               int64  `json:"failed"`
	Complete             bool   `json:"complete"`
}

func (h *Stats) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()
	s, err := h.Repo.Stats(ctx)
	if err != nil {
		readFailed(w, r, h.Log, "read stats", err)
		return
	}
	helpers.WriteJSON(w, http.StatusOK, newStatsJSON(s))
}

func newStatsJSON(s StatsSnapshot) statsJSON {
	in := s.Inbox
	out := statsJSON{
		AsOf: jsonTime(s.AsOf),
		Inbox: inboxStatsJSON{
			Received: in.Received, Pending: in.Pending,
			Applied: in.Applied, Quarantined: in.Quarantined, Failed: in.Failed,
			OldestPendingAt:         (*jsonTime)(in.OldestPendingAt),
			OldestPendingAgeSeconds: pendingAge(s.AsOf, in.OldestPendingAt),
		},
		Rockets: make([]rocketStatsJSON, 0, len(s.Channels)),
	}
	for _, c := range s.Channels {
		if !domain.ValidChannel(c.Channel) { // counted in the totals only
			continue
		}
		missing := c.Highest - c.Received
		out.Rockets = append(out.Rockets, rocketStatsJSON{
			Channel: c.Channel, HighestMessageNumber: c.Highest, Received: c.Received, Missing: missing,
			Pending: c.Pending, Applied: c.Applied, Quarantined: c.Quarantined, Failed: c.Failed,
			Complete: missing == 0 && c.Pending == 0 && c.Failed == 0,
		})
	}
	slices.SortFunc(out.Rockets, func(a, b rocketStatsJSON) int { return cmp.Compare(a.Channel, b.Channel) })
	return out
}

// pendingAge is the age of the oldest pending entry at asOf in seconds,
// rounded to milliseconds; both times come from the database clock (design D3).
func pendingAge(asOf time.Time, oldest *time.Time) float64 {
	if oldest == nil {
		return 0
	}
	ms := math.Max(0, float64(asOf.Sub(*oldest).Round(time.Millisecond).Milliseconds()))
	return ms / 1000
}
