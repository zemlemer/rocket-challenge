package postgres_test

import (
	"context"
	"testing"
)

func TestInboxSchema(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	insert := func(channel any, number any, body string) error {
		_, err := pool.Exec(ctx,
			`insert into inbox (channel, message_number, body, truncated) values ($1, $2, $3, false)`,
			channel, number, []byte(body))
		return err
	}

	if err := insert("c1", 1, `{"a":1}`); err != nil {
		t.Fatalf("entry with identity: %v", err)
	}
	if err := insert(nil, nil, `not json`); err != nil {
		t.Fatalf("entry without identity: %v", err)
	}
	if err := insert(nil, nil, `not json`); err != nil {
		t.Fatalf("second entry without identity: %v", err)
	}
	if err := insert("c1", 1, `other body`); err == nil {
		t.Fatal("duplicate identity was accepted")
	}
	if err := insert("c1", nil, `x`); err == nil {
		t.Fatal("half an identity (channel only) was accepted")
	}
	if err := insert(nil, 7, `x`); err == nil {
		t.Fatal("half an identity (number only) was accepted")
	}

	rows, err := pool.Query(ctx, `select id, received_at is not null from inbox order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var last int64
	n := 0
	for rows.Next() {
		var id int64
		var hasTime bool
		if err := rows.Scan(&id, &hasTime); err != nil {
			t.Fatal(err)
		}
		if id <= last || !hasTime {
			t.Fatalf("ids must increase and receipt time be set: id=%d last=%d time=%v", id, last, hasTime)
		}
		last = id
		n++
	}
	if n != 3 {
		t.Fatalf("inbox holds %d entries, want 3", n)
	}
}
