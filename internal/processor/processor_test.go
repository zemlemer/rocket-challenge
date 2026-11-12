package processor_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"rockets/internal/processor"
	"rockets/internal/testdb"
)

const ms = time.Millisecond

func run(t *testing.T, script []step, opts ...processor.Option) (*fakeRepo, *clock) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	repo := &fakeRepo{script: script, cancel: cancel}
	c := &clock{}
	p := processor.New(repo, testdb.Logger(), append([]processor.Option{processor.WithSleep(c.sleep)}, opts...)...)
	done := make(chan error)
	go func() { done <- p.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	return repo, c
}

func TestSleepsOnlyAfterEmptyBatch(t *testing.T) {
	_, c := run(t, []step{{n: 5}, {n: 3}, {n: 0}, {n: 2}, {n: 0}})
	want := []time.Duration{processor.PollInterval, processor.PollInterval}
	if !reflect.DeepEqual(c.sleeps[:2], want) || len(c.sleeps) != 3 { // + the final empty batch
		t.Fatalf("sleeps = %v", c.sleeps)
	}
}

func TestBackoffOnUnavailableAndReset(t *testing.T) {
	down := fmt.Errorf("conn refused: %w", processor.ErrUnavailable)
	_, c := run(t, []step{{err: down}, {err: down}, {err: down}, {err: down}, {err: down}, {err: down}, {n: 1}, {err: down}})
	want := []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 2000 * ms, 100 * ms}
	if !reflect.DeepEqual(c.sleeps[:len(want)], want) {
		t.Fatalf("sleeps = %v, want %v", c.sleeps, want)
	}
}

func TestOtherErrorsAlsoBackOff(t *testing.T) {
	_, c := run(t, []step{{err: errors.New("bug")}, {err: errors.New("bug")}})
	if c.sleeps[0] != 100*ms || c.sleeps[1] != 200*ms {
		t.Fatalf("sleeps = %v", c.sleeps)
	}
}

func TestFixedAndOverriddenBatchSize(t *testing.T) {
	repo, _ := run(t, []step{{n: 1}})
	if repo.limits[0] != processor.BatchSize || processor.BatchSize != 1000 {
		t.Fatalf("limit = %d", repo.limits[0])
	}
	repo, _ = run(t, []step{{n: 1}}, processor.WithBatchSize(10))
	if repo.limits[0] != 10 {
		t.Fatalf("limit = %d", repo.limits[0])
	}
}

func TestStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := processor.New(&fakeRepo{cancel: func() {}}, testdb.Logger())
	if err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
