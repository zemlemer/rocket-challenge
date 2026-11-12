// Package processor works through the inbox: it claims unprocessed entries in
// batches, folds them into rocket state and marks them, via the Repository it
// declares. It knows no SQL and no HTTP.
package processor

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"rockets/internal/domain"
	"rockets/pkg/helpers"
)

// Fixed settings (design D9a): nothing needs to change them in operation.
const (
	BatchSize       = 1000                   // entries claimed per transaction
	PollInterval    = 100 * time.Millisecond // pause after an empty batch
	ShutdownTimeout = 15 * time.Second       // bound for finishing after a signal
	batchTimeout    = 10 * time.Second       // a batch takes milliseconds normally
)

// ErrUnavailable marks storage failures worth retrying after a pause: nothing
// was marked, the batch can simply be claimed again.
var ErrUnavailable = errors.New("storage unavailable")

// Repository is what the processor needs from storage.
type Repository interface {
	// ProcessBatch claims up to limit unprocessed entries in one transaction,
	// passes them to fold, applies the resulting deltas, marks every entry as
	// decided and commits. It returns the number of entries processed.
	ProcessBatch(ctx context.Context, limit int, fold func([]domain.InboxEntry) domain.BatchResult) (int, error)
}

// Processor runs the processing loop.
type Processor struct {
	repo         Repository
	log          *slog.Logger
	batchSize    int
	pollInterval time.Duration
	sleep        func(ctx context.Context, d time.Duration)
}

// Option adjusts a Processor; used by tests.
type Option func(*Processor)

// WithBatchSize sets the number of entries claimed per transaction.
func WithBatchSize(n int) Option { return func(p *Processor) { p.batchSize = n } }

// WithPollInterval sets the pause after an empty batch.
func WithPollInterval(d time.Duration) Option { return func(p *Processor) { p.pollInterval = d } }

// WithSleep replaces the pause function (tests inject a fake clock).
func WithSleep(f func(ctx context.Context, d time.Duration)) Option {
	return func(p *Processor) { p.sleep = f }
}

// New returns a processor with the fixed settings.
func New(repo Repository, log *slog.Logger, opts ...Option) *Processor {
	p := &Processor{repo: repo, log: log, batchSize: BatchSize, pollInterval: PollInterval, sleep: sleepCtx}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Run processes batches until ctx is cancelled. A batch in progress when ctx
// is cancelled still commits or rolls back on its own (detached context with
// a bounded timeout); Run then returns.
func (p *Processor) Run(ctx context.Context) error {
	attempt := 0
	for ctx.Err() == nil {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)
		n, err := p.repo.ProcessBatch(bctx, p.batchSize, domain.Fold)
		cancel()

		if err != nil {
			wait := helpers.Backoff(attempt)
			attempt++
			if errors.Is(err, ErrUnavailable) {
				p.log.Warn("storage unavailable, retrying", "retry_in", wait, "error", err)
			} else {
				p.log.Error("batch failed, retrying", "retry_in", wait, "error", err)
			}
			p.sleep(ctx, wait)
			continue
		}
		attempt = 0
		if n == 0 {
			p.sleep(ctx, p.pollInterval)
			continue
		}
		p.log.Debug("batch processed", "entries", n)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
