// Package intake implements the storage interfaces declared by the
// handler package, using SQL over a PostgreSQL connection pool.
package intake

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/handler"
)

// Inbox stores accepted requests in the inbox table.
type Inbox struct {
	pool *pgxpool.Pool
}

var _ handler.InboxRepository = (*Inbox)(nil)

// NewInbox returns an inbox repository over pool.
func NewInbox(pool *pgxpool.Pool) *Inbox { return &Inbox{pool: pool} }

// Save inserts the entry; an entry whose identity is already in the inbox is
// silently skipped, so the first stored body wins.
func (r *Inbox) Save(ctx context.Context, e handler.InboxEntry) error {
	var channel *string
	var number *int64
	if e.Identity != nil {
		channel, number = &e.Identity.Channel, &e.Identity.MessageNumber
	}
	body := e.Body
	if body == nil {
		body = []byte{} // column is NOT NULL; an empty body is a valid entry
	}
	_, err := r.pool.Exec(ctx, `
		insert into inbox (channel, message_number, message_time, message_type, body, truncated)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (channel, message_number) where channel is not null do nothing`,
		channel, number, e.MessageTime, e.MessageType, body, e.Truncated)
	return classify("save inbox entry", err)
}
