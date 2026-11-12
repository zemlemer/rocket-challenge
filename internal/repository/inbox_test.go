package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rockets/internal/handler"
	"rockets/internal/repository"
	"rockets/internal/testdb"
)

func TestSaveStoresEntries(t *testing.T) {
	p := testdb.Pool(t, migratedDB(t))
	repo := repository.NewInbox(p)
	ctx := context.Background()

	for _, e := range []handler.InboxEntry{
		{Identity: id("c1", 1), Body: []byte(`first`)},
		{Body: []byte(`not json`)},
		{Body: nil},                                     // empty body
		{Body: []byte(`cut`), Truncated: true},          // truncated, no identity
		{Identity: id("c1", 1), Body: []byte(`second`)}, // duplicate identity
	} {
		if err := repo.Save(ctx, e); err != nil {
			t.Fatalf("Save(%+v): %v", e, err)
		}
	}

	got := rows(t, p)
	if len(got) != 4 {
		t.Fatalf("inbox holds %d entries, want 4", len(got))
	}
	if *got[0].channel != "c1" || *got[0].number != 1 || string(got[0].body) != "first" {
		t.Errorf("first entry = %+v; duplicate must keep the first body", got[0])
	}
	if got[1].channel != nil || got[1].number != nil {
		t.Errorf("entry without identity got one: %+v", got[1])
	}
	if len(got[2].body) != 0 {
		t.Errorf("empty body stored as %q", got[2].body)
	}
	if !got[3].truncated {
		t.Errorf("truncation flag lost")
	}
}

func TestConcurrentIdenticalSavesStoreOneRow(t *testing.T) {
	p := testdb.Pool(t, migratedDB(t))
	repo := repository.NewInbox(p)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Save(context.Background(), handler.InboxEntry{Metadata: withID("c", 7), Body: []byte(`m7`)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := len(rows(t, p)); n != 1 {
		t.Fatalf("inbox holds %d rows, want 1", n)
	}
}

// Two independent pools behave like two server instances.
func TestTwoInstancesStoreEachIdentityOnce(t *testing.T) {
	url := migratedDB(t)
	repos := []*repository.Inbox{repository.NewInbox(testdb.Pool(t, url)), repository.NewInbox(testdb.Pool(t, url))}

	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := int64(1); n <= 50; n++ {
				body := []byte{byte('a' + i)}
				if err := repo.Save(context.Background(), handler.InboxEntry{Metadata: withID("c", n), Body: body}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()

	got := rows(t, testdb.Pool(t, url))
	if len(got) != 50 {
		t.Fatalf("inbox holds %d rows, want 50", len(got))
	}
	seen := map[int64]bool{}
	for _, r := range got {
		if seen[*r.number] {
			t.Fatalf("identity %d stored twice", *r.number)
		}
		seen[*r.number] = true
	}
}

func TestErrorClassification(t *testing.T) {
	url := migratedDB(t)

	t.Run("closed pool is unavailable", func(t *testing.T) {
		p := testdb.Pool(t, url)
		p.Close()
		repo := repository.NewInbox(p)
		if err := repo.Save(context.Background(), handler.InboxEntry{Body: []byte(`x`)}); !errors.Is(err, handler.ErrUnavailable) {
			t.Fatalf("Save err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("statement error is not unavailable", func(t *testing.T) {
		// Bypasses identity extraction on purpose: PostgreSQL rejects NUL in text.
		repo := repository.NewInbox(testdb.Pool(t, url))
		err := repo.Save(context.Background(), handler.InboxEntry{Metadata: withID("a\x00b", 1), Body: []byte(`x`)})
		if err == nil || errors.Is(err, handler.ErrUnavailable) {
			t.Fatalf("err = %v, want a non-unavailable error", err)
		}
	})
}

func TestSaveStoresMetadataColumns(t *testing.T) {
	p := testdb.Pool(t, migratedDB(t))
	at := time.Date(2022, 2, 2, 18, 39, 5, 863370000, time.UTC)
	typ := "RocketTeleported"
	md := handler.Metadata{Identity: id("c9", 4), MessageTime: &at, MessageType: &typ}
	if err := repository.NewInbox(p).Save(context.Background(), handler.InboxEntry{Metadata: md, Body: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewInbox(p).Save(context.Background(), handler.InboxEntry{Body: []byte(`x`)}); err != nil {
		t.Fatal(err)
	}

	var gotTime []*time.Time
	var gotType []*string
	rs, err := p.Query(context.Background(), `select message_time, message_type from inbox order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	for rs.Next() {
		var mt *time.Time
		var ty *string
		if err := rs.Scan(&mt, &ty); err != nil {
			t.Fatal(err)
		}
		gotTime, gotType = append(gotTime, mt), append(gotType, ty)
	}
	if gotTime[0] == nil || !gotTime[0].Equal(at) || gotType[0] == nil || *gotType[0] != typ {
		t.Fatalf("first entry: time=%v type=%v", gotTime[0], gotType[0])
	}
	if gotTime[1] != nil || gotType[1] != nil {
		t.Fatalf("entry without metadata stored time=%v type=%v", gotTime[1], gotType[1])
	}
}
