package intake

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/handler"
)

// Rockets reads rocket state for the query API. Its queries take no locks,
// so they never block processing.
type Rockets struct {
	pool *pgxpool.Pool
}

var _ handler.RocketRepository = (*Rockets)(nil)

// NewRockets returns a rocket read repository over pool.
func NewRockets(pool *pgxpool.Pool) *Rockets { return &Rockets{pool: pool} }

const rocketColumns = `
	channel, speed, rocket_type, launch_speed, launched_at, mission, mission_message_number,
	explosion_reason, last_applied_message_number, last_applied_message_at`

// visible is the condition for a rocket to be readable (design D1).
const visible = `(rocket_type is not null or explosion_reason is not null)`

// sortKeys maps each sort field to its SQL expression. Only these fixed
// strings ever reach ORDER BY; text compares byte-wise.
var sortKeys = map[handler.SortField]string{
	handler.SortChannel:    `channel collate "C"`,
	handler.SortType:       `rocket_type collate "C"`,
	handler.SortMission:    `mission collate "C"`,
	handler.SortSpeed:      `speed`,
	handler.SortStatus:     `(explosion_reason is not null)`, // false (active) first ascending
	handler.SortLaunchedAt: `launched_at`,
}

// Rocket returns the visible rocket with exactly this channel.
func (r *Rockets) Rocket(ctx context.Context, channel string) (domain.Rocket, bool, error) {
	rows, err := r.pool.Query(ctx, `select `+rocketColumns+` from rockets where channel = $1 and `+visible, channel)
	if err != nil {
		return domain.Rocket{}, false, classify("read rocket", err)
	}
	rocket, err := pgx.CollectExactlyOneRow(rows, scanRocket)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Rocket{}, false, nil
	}
	if err != nil {
		return domain.Rocket{}, false, classify("read rocket", err)
	}
	return rocket, true, nil
}

// Rockets returns every visible rocket in the order of q.
func (r *Rockets) Rockets(ctx context.Context, q handler.RocketQuery) ([]domain.Rocket, error) {
	key, ok := sortKeys[q.Sort]
	if !ok {
		return nil, fmt.Errorf("list rockets: unknown sort field %q", q.Sort)
	}
	dir := "asc"
	if q.Order == handler.Desc {
		dir = "desc"
	}
	rows, err := r.pool.Query(ctx, `select `+rocketColumns+` from rockets where `+visible+
		` order by `+key+` `+dir+` nulls last, channel collate "C" asc`)
	if err != nil {
		return nil, classify("list rockets", err)
	}
	rockets, err := pgx.CollectRows(rows, scanRocket)
	if err != nil {
		return nil, classify("list rockets", err)
	}
	if rockets == nil {
		rockets = []domain.Rocket{}
	}
	return rockets, nil
}

func scanRocket(row pgx.CollectableRow) (domain.Rocket, error) {
	var r domain.Rocket
	err := row.Scan(&r.Channel, &r.Speed, &r.RocketType, &r.LaunchSpeed, &r.LaunchedAt, &r.Mission,
		&r.MissionNumber, &r.ExplosionReason, &r.LastAppliedNumber, &r.LastAppliedAt)
	return r, err
}
