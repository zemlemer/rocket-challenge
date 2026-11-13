package handler

import (
	"context"
	"slices"
	"strings"

	"rockets/internal/domain"
)

// SortField names a field GET /rockets can be ordered by.
type SortField string

// Sort fields accepted by GET /rockets.
const (
	SortChannel    SortField = "channel"
	SortType       SortField = "type"
	SortMission    SortField = "mission"
	SortSpeed      SortField = "speed"
	SortStatus     SortField = "status"
	SortLaunchedAt SortField = "launchedAt"
)

// sortFields lists the accepted sort fields in the order they are documented;
// sortError, the 400 message for any other value, is derived from it.
var (
	sortFields = []SortField{SortChannel, SortType, SortMission, SortSpeed, SortStatus, SortLaunchedAt}
	sortError  = func() string {
		names := make([]string, len(sortFields))
		for i, f := range sortFields {
			names[i] = string(f)
		}
		return "sort: must be given once as one of " + strings.Join(names, ", ")
	}()
)

// SortFields returns the accepted sort fields; storage implementations must
// support every one of them.
func SortFields() []SortField { return slices.Clone(sortFields) }

// SortOrder is the direction of a sort.
type SortOrder string

// Sort directions accepted by GET /rockets.
const (
	Asc  SortOrder = "asc"
	Desc SortOrder = "desc"
)

// RocketQuery selects the order of a rocket list.
type RocketQuery struct {
	Sort  SortField
	Order SortOrder
}

// RocketRepository is what the rocket read handlers need from storage. It
// returns only rockets whose launch or explosion has been processed; it does
// not apply the channel rule, which the handlers own (design D5).
type RocketRepository interface {
	// Rocket returns the rocket with exactly this channel; false when there
	// is none or it is neither launched nor exploded.
	Rocket(ctx context.Context, channel string) (domain.Rocket, bool, error)
	// Rockets returns every launched or exploded rocket in the order of q:
	// NULL values last in either direction, ties by ascending channel.
	Rockets(ctx context.Context, q RocketQuery) ([]domain.Rocket, error)
}
