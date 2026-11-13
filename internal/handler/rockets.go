package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"rockets/internal/domain"
	"rockets/pkg/helpers"
)

// readTimeout bounds one read query; a slower database answers 503.
const readTimeout = 5 * time.Second

const errRocketNotFound = "rocket not found"

// rocketJSON is the public representation of a rocket, the same in both read
// endpoints. Every field is always present; launch fields are null until the
// launch is processed.
type rocketJSON struct {
	Channel           string    `json:"channel"`
	Type              *string   `json:"type"`
	Status            string    `json:"status"`
	Speed             int64     `json:"speed"`
	LaunchSpeed       *int64    `json:"launchSpeed"`
	Mission           *string   `json:"mission"`
	LaunchedAt        *jsonTime `json:"launchedAt"`
	ExplosionReason   *string   `json:"explosionReason"`
	LastMessageNumber int64     `json:"lastMessageNumber"`
	LastMessageAt     *jsonTime `json:"lastMessageAt"`
}

// Rocket statuses.
const (
	statusActive   = "active"
	statusExploded = "exploded"
)

// jsonTime writes a time in UTC as RFC 3339 with microsecond precision (truncated, as stored).
// Unlike time.Time.MarshalJSON it never fails: a year outside 0000–9999 is
// written with all its digits (design D3).
type jsonTime time.Time

func (t jsonTime) MarshalJSON() ([]byte, error) {
	s := time.Time(t).UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	return []byte(strconv.Quote(s)), nil
}

// newRocketJSON converts stored rocket state to its public representation.
func newRocketJSON(r domain.Rocket) rocketJSON {
	status := statusActive
	if r.ExplosionReason != nil {
		status = statusExploded
	}
	return rocketJSON{
		Channel:           r.Channel,
		Type:              r.RocketType,
		Status:            status,
		Speed:             r.Speed,
		LaunchSpeed:       r.LaunchSpeed,
		Mission:           r.Mission,
		LaunchedAt:        (*jsonTime)(r.LaunchedAt),
		ExplosionReason:   r.ExplosionReason,
		LastMessageNumber: r.LastAppliedNumber,
		LastMessageAt:     (*jsonTime)(r.LastAppliedAt),
	}
}

// Rockets serves the read endpoints: Get for GET /rockets/{channel} and List
// for GET /rockets.
type Rockets struct {
	Repo RocketRepository
	Log  *slog.Logger
}

// rocketList is the body of GET /rockets; an object so that fields such as
// pagination can be added later without breaking clients.
type rocketList struct {
	Rockets []rocketJSON `json:"rockets"`
}

// Get handles GET /rockets/{channel}.
func (h *Rockets) Get(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	if !domain.ValidChannel(channel) { // hidden or impossible: no query needed
		helpers.WriteError(w, http.StatusNotFound, errRocketNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()
	rocket, ok, err := h.Repo.Rocket(ctx, channel)
	if err != nil {
		readFailed(w, r, h.Log, "read rocket", err)
		return
	}
	if !ok {
		helpers.WriteError(w, http.StatusNotFound, errRocketNotFound)
		return
	}
	helpers.WriteJSON(w, http.StatusOK, newRocketJSON(rocket))
}

// List handles GET /rockets.
func (h *Rockets) List(w http.ResponseWriter, r *http.Request) {
	q, msg := parseRocketQuery(r.URL.RawQuery)
	if msg != "" {
		helpers.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()
	rockets, err := h.Repo.Rockets(ctx, q)
	if err != nil {
		readFailed(w, r, h.Log, "list rockets", err)
		return
	}
	list := rocketList{Rockets: make([]rocketJSON, 0, len(rockets))}
	for _, rocket := range rockets {
		if domain.ValidChannel(rocket.Channel) { // hide rows stored before the channel rule (design D5)
			list.Rockets = append(list.Rockets, newRocketJSON(rocket))
		}
	}
	helpers.WriteJSON(w, http.StatusOK, list)
}
