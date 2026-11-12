package postgres

import (
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionLimits bound how long a session may hold locks. They are "something
// is wrong" thresholds, not tuning (design D2 of add-message-processing).
type SessionLimits struct {
	IdleInTransaction time.Duration // Postgres ends a session idle inside a transaction
	Statement         time.Duration // no statement runs longer
	Lock              time.Duration // no statement waits longer for a lock
}

// ProcessorLimits are the limits of the processor's sessions.
var ProcessorLimits = SessionLimits{IdleInTransaction: 30 * time.Second, Statement: 30 * time.Second, Lock: 5 * time.Second}

// Keepalive settings that detect a dead connection in about a minute
// instead of the operating system's default of about two hours.
const (
	keepaliveIdle     = 30 * time.Second
	keepaliveInterval = 10 * time.Second
	keepaliveCount    = 3
)

// WithSessionLimits sets the limits as session parameters of every
// connection of cfg and enables TCP keepalive.
func WithSessionLimits(cfg *pgxpool.Config, l SessionLimits) {
	params := cfg.ConnConfig.RuntimeParams
	params["idle_in_transaction_session_timeout"] = millis(l.IdleInTransaction)
	params["statement_timeout"] = millis(l.Statement)
	params["lock_timeout"] = millis(l.Lock)

	dialer := &net.Dialer{
		Timeout: cfg.ConnConfig.ConnectTimeout,
		KeepAliveConfig: net.KeepAliveConfig{
			Enable: true, Idle: keepaliveIdle, Interval: keepaliveInterval, Count: keepaliveCount,
		},
	}
	cfg.ConnConfig.DialFunc = dialer.DialContext
}

func millis(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
