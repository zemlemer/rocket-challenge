package processor_test

import (
	"context"
	"sync"
	"time"
)

// step is one scripted ProcessBatch result.
type step struct {
	n   int
	err error
}

// fakeRepo returns scripted results and cancels the run when the script ends.
type fakeRepo struct {
	mu     sync.Mutex
	script []step
	limits []int
	cancel context.CancelFunc
}

func (f *fakeRepo) ProcessBatch(_ context.Context, limit int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	if len(f.script) == 0 {
		f.cancel()
		return 0, nil
	}
	s := f.script[0]
	f.script = f.script[1:]
	return s.n, s.err
}

// clock records requested sleeps without sleeping.
type clock struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func (c *clock) sleep(_ context.Context, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
}
