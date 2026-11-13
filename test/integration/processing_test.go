package integration

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rockets/internal/domain"
	"rockets/internal/handler"
	"rockets/internal/postgres"
	"rockets/internal/processor"
	"rockets/internal/repository/intake"
	"rockets/internal/repository/processing"
	"rockets/internal/testdb"
)

// runProcessors starts n processors and returns a stop function.
func runProcessors(t *testing.T, repo processor.Repository, n int) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := processor.New(repo, testdb.Logger(), processor.WithBatchSize(7), processor.WithPollInterval(5*time.Millisecond))
			_ = p.Run(ctx)
		}()
	}
	stopped := false
	stop := func() {
		if !stopped {
			cancel()
			wg.Wait()
			stopped = true
		}
	}
	t.Cleanup(stop)
	return stop
}

// Spec message-processing "Four processors, shuffled stream" and
// "Concurrent processors": entries arrive while four processors run.
func TestFourProcessorsMatchReference(t *testing.T) {
	url, p := testdb.Migrated(t)
	ms := messages(rand.New(rand.NewPCG(1, 2)), 6, 60)
	insertAll(t, p, ms[:len(ms)/2])
	stop := runProcessors(t, processing.New(limitedPool(t, url, postgres.ProcessorLimits), testdb.Logger()), 4)
	insertAll(t, p, ms[len(ms)/2:])
	waitProcessed(t, p, 30*time.Second)
	stop()
	assertMatchesReference(t, p)
}

// stopAfterClaim stops the proxy once the claim query has returned its rows,
// once armed: the batch holds its entries but has not updated anything yet.
type stopAfterClaim struct {
	proxy *testdb.Proxy
	armed atomic.Bool
}

func (s *stopAfterClaim) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (s *stopAfterClaim) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil && strings.Contains(data.CommandTag.String(), "SELECT") && s.armed.CompareAndSwap(true, false) {
		s.proxy.Stop()
	}
}

// Spec "Crash safety": the database goes away in the middle of a batch; the
// batch has no effect and a re-run applies everything once.
func TestBatchInterruptedMidwayIsReappliedOnce(t *testing.T) {
	url, p := testdb.Migrated(t)
	insertAll(t, p, messages(rand.New(rand.NewPCG(3, 4)), 3, 20))
	proxy, proxied := testdb.NewProxy(t, url)
	breaker := &stopAfterClaim{proxy: proxy}
	cfg, err := postgres.PoolConfig(proxied)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = breaker
	traced, err := postgres.ConnectConfig(context.Background(), cfg, 10*time.Second, testdb.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	repo := processing.New(traced, testdb.Logger())

	breaker.armed.Store(true)
	_, err = repo.ProcessBatch(context.Background(), 1000)
	if breaker.armed.Load() {
		t.Fatal("proxy was not stopped after the claim")
	}
	if !errors.Is(err, processor.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if unprocessed(t, p) != 60 {
		t.Fatalf("interrupted batch marked entries: %d unprocessed", unprocessed(t, p))
	}
	var rockets int
	_ = p.QueryRow(context.Background(), `select count(*) from rockets`).Scan(&rockets)
	if rockets != 0 {
		t.Fatalf("interrupted batch left %d rockets", rockets)
	}

	proxy.Start()
	for {
		n, err := repo.ProcessBatch(context.Background(), 1000)
		if err != nil && !errors.Is(err, processor.ErrUnavailable) {
			t.Fatal(err)
		}
		if err == nil && n == 0 {
			break
		}
	}
	assertMatchesReference(t, p)
}

// killBeforeCommit terminates the session that is about to send COMMIT, once
// armed. It relies on pgx sending COMMIT as a query of its own.
type killBeforeCommit struct {
	admin *pgxpool.Pool
	armed atomic.Bool
	err   error // of pg_terminate_backend; read after ProcessBatch returns
}

func (k *killBeforeCommit) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") && k.armed.CompareAndSwap(true, false) {
		var ok bool
		// The timeout makes pg_terminate_backend wait until the session is
		// gone, so COMMIT never reaches it.
		k.err = k.admin.QueryRow(context.WithoutCancel(ctx), `select pg_terminate_backend($1, 5000)`, conn.PgConn().PID()).Scan(&ok)
		if k.err == nil && !ok {
			k.err = errors.New("session not terminated")
		}
	}
	return ctx
}

func (*killBeforeCommit) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Spec "Crash safety", failure at the last moment: the session dies after the
// batch updated the rockets and marked its entries, right before COMMIT. The
// batch has no effect, and a re-run applies every entry once.
func TestBatchKilledBeforeCommitIsRolledBack(t *testing.T) {
	url, p := testdb.Migrated(t)
	ctx := context.Background()
	repo := processing.New(p, testdb.Logger())
	testdb.InsertEntry(t, p, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":100,"mission":"M"}`)
	testdb.Drain(t, repo, 10)
	testdb.InsertEntry(t, p, "a", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":1,"mission":"M"}`)
	testdb.InsertEntry(t, p, "c", 2, domain.TypeSpeedIncreased, `{"by":5}`)

	killer := &killBeforeCommit{admin: p}
	cfg, err := postgres.PoolConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = killer
	traced, err := postgres.ConnectConfig(ctx, cfg, 10*time.Second, testdb.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	repo = processing.New(traced, testdb.Logger())

	killer.armed.Store(true)
	_, err = repo.ProcessBatch(ctx, 1000)
	if killer.err != nil || killer.armed.Load() {
		t.Fatalf("session was not terminated before COMMIT: armed=%v err=%v", killer.armed.Load(), killer.err)
	}
	if !errors.Is(err, processor.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	var rocketA int
	if err := p.QueryRow(ctx, `select count(*) from rockets where channel = 'a'`).Scan(&rocketA); err != nil {
		t.Fatal(err)
	}
	if rocketA != 0 || testdb.LoadRocket(t, p, "c").Speed != 100 || unprocessed(t, p) != 2 {
		t.Fatalf("killed batch had an effect: rocket a rows=%d, c speed=%d, unprocessed=%d",
			rocketA, testdb.LoadRocket(t, p, "c").Speed, unprocessed(t, p))
	}

	if n, err := repo.ProcessBatch(ctx, 1000); err != nil || n != 2 {
		t.Fatalf("re-run: n=%d err=%v, want 2", n, err)
	}
	if n, err := repo.ProcessBatch(ctx, 1000); err != nil || n != 0 {
		t.Fatalf("after re-run: n=%d err=%v, want nothing left", n, err)
	}
	if testdb.LoadRocket(t, p, "c").Speed != 105 || testdb.LoadRocket(t, p, "a").Speed != 1 {
		t.Fatalf("re-run: a=%+v c=%+v", testdb.LoadRocket(t, p, "a"), testdb.LoadRocket(t, p, "c"))
	}
}

// Spec "Recovery from storage outage": a running processor resumes by itself.
func TestProcessorResumesAfterOutage(t *testing.T) {
	url, p := testdb.Migrated(t)
	proxy, proxied := testdb.NewProxy(t, url)
	stop := runProcessors(t, processing.New(testdb.Pool(t, proxied), testdb.Logger()), 1)
	proxy.Stop()
	insertAll(t, p, messages(rand.New(rand.NewPCG(5, 6)), 2, 15))
	time.Sleep(time.Second)
	if unprocessed(t, p) != 30 {
		t.Fatal("entries processed while the database was unreachable")
	}
	proxy.Start()
	waitProcessed(t, p, 10*time.Second)
	stop()
	assertMatchesReference(t, p)
}

// Spec "Stuck processors do not block others": a session that claims a batch
// and then hangs loses its claim when Postgres ends the idle transaction.
func TestStuckTransactionLosesItsClaim(t *testing.T) {
	url, p := testdb.Migrated(t)
	insertAll(t, p, messages(rand.New(rand.NewPCG(7, 8)), 2, 10))
	short := postgres.SessionLimits{IdleInTransaction: time.Second, Statement: 30 * time.Second, Lock: 5 * time.Second}
	stuckPool := limitedPool(t, url, short)
	ctx := context.Background()

	stuck, err := stuckPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stuck.Exec(ctx, `select id from inbox where processed_at is null for update skip locked`); err != nil {
		t.Fatal(err)
	}
	// The stuck "processor" now does nothing.

	repo := processing.New(testdb.Pool(t, url), testdb.Logger())
	if n, err := repo.ProcessBatch(ctx, 1000); err != nil || n != 0 {
		t.Fatalf("claimed entries locked by the stuck session: n=%d err=%v", n, err)
	}
	time.Sleep(1500 * time.Millisecond) // > idle_in_transaction_session_timeout
	if n, err := repo.ProcessBatch(ctx, 1000); err != nil || n != 20 {
		t.Fatalf("after the timeout: n=%d err=%v, want 20", n, err)
	}
	if err := stuck.Commit(ctx); err == nil {
		t.Fatal("the stuck transaction could still commit")
	}
	assertMatchesReference(t, p)
}

// A processor blocked on a locked rocket row gives up after lock_timeout,
// marks nothing, and succeeds once the row is free.
func TestBlockedOnRocketRowBacksOff(t *testing.T) {
	url, p := testdb.Migrated(t)
	ctx := context.Background()
	testdb.InsertEntry(t, p, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":1,"mission":"M"}`)
	repo := processing.New(limitedPool(t, url, postgres.SessionLimits{IdleInTransaction: 30 * time.Second, Statement: 30 * time.Second, Lock: 300 * time.Millisecond}), testdb.Logger())
	if _, err := repo.ProcessBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	testdb.InsertEntry(t, p, "c", 2, domain.TypeSpeedIncreased, `{"by":5}`)

	holder, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select 1 from rockets where channel = 'c' for update`); err != nil {
		t.Fatal(err)
	}
	_, err = repo.ProcessBatch(ctx, 10)
	if !errors.Is(err, processor.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable after lock_timeout", err)
	}
	if unprocessed(t, p) != 1 {
		t.Fatal("entry was marked although its batch gave up")
	}
	_ = holder.Rollback(ctx)
	if n, err := repo.ProcessBatch(ctx, 10); err != nil || n != 1 {
		t.Fatalf("after release: n=%d err=%v", n, err)
	}
	if testdb.LoadRocket(t, p, "c").Speed != 6 {
		t.Fatal("increment not applied")
	}
}

// Spec message-intake (modified) "Redelivery after processing".
func TestRedeliveryOfProcessedMessageIsDroppedAtIntake(t *testing.T) {
	_, p := testdb.Migrated(t)
	inbox := &handler.Messages{Repo: intake.NewInbox(p), Log: testdb.Logger()}
	const body = `{"metadata":{"channel":"c","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"A","launchSpeed":500,"mission":"M"}}`
	post := func() int {
		rec := httptest.NewRecorder()
		inbox.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body)))
		return rec.Code
	}
	if post() != http.StatusAccepted {
		t.Fatal("first delivery not accepted")
	}
	repo := processing.New(p, testdb.Logger())
	if _, err := repo.ProcessBatch(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if post() != http.StatusAccepted {
		t.Fatal("redelivery not answered 202")
	}
	var entries int
	_ = p.QueryRow(context.Background(), `select count(*) from inbox`).Scan(&entries)
	if entries != 1 || unprocessed(t, p) != 0 || testdb.LoadRocket(t, p, "c").Speed != 500 {
		t.Fatalf("entries=%d unprocessed=%d speed=%d", entries, unprocessed(t, p), testdb.LoadRocket(t, p, "c").Speed)
	}
}

func TestInvalidChannelIsQuarantinedAfterIntake(t *testing.T) {
	_, p := testdb.Migrated(t)
	ctx := context.Background()
	inbox := &handler.Messages{Repo: intake.NewInbox(p), Log: testdb.Logger()}
	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		inbox.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body)))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST status = %d, want 202", rec.Code)
		}
	}
	const invalid = `{"metadata":{"channel":"bad/channel","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"A","launchSpeed":500,"mission":"M"}}`
	const validChannel = "193270a9-c9cf-404a-8f83-838e71d9ae67"
	const speedBeforeLaunch = `{"metadata":{"channel":"` + validChannel + `","messageNumber":2,"messageTime":"2022-02-02T19:39:06Z","messageType":"RocketSpeedIncreased"},"message":{"by":300}}`
	const launch = `{"metadata":{"channel":"` + validChannel + `","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"A","launchSpeed":500,"mission":"M"}}`

	post(invalid)
	post(invalid)
	post(speedBeforeLaunch)
	post(launch)
	var invalidEntries int
	if err := p.QueryRow(ctx, `select count(*) from inbox where channel = 'bad/channel'`).Scan(&invalidEntries); err != nil {
		t.Fatal(err)
	}
	if invalidEntries != 1 {
		t.Fatalf("invalid channel entries = %d, want 1", invalidEntries)
	}

	repo := processing.New(p, testdb.Logger())
	if n, err := repo.ProcessBatch(ctx, 10); err != nil || n != 3 {
		t.Fatalf("processed n=%d err=%v, want 3 entries", n, err)
	}
	var outcome, reason string
	if err := p.QueryRow(ctx, `select outcome, reason from inbox where channel = 'bad/channel'`).Scan(&outcome, &reason); err != nil {
		t.Fatal(err)
	}
	if outcome != "quarantined" || !strings.Contains(reason, "metadata.channel") {
		t.Fatalf("invalid channel outcome=%q reason=%q", outcome, reason)
	}
	post(invalid) // redelivery after quarantine must not create a pending copy
	var entriesAfter, quarantinedAfter int
	if err := p.QueryRow(ctx, `select count(*), count(*) filter (where outcome = 'quarantined') from inbox where channel = 'bad/channel'`).Scan(&entriesAfter, &quarantinedAfter); err != nil {
		t.Fatal(err)
	}
	if entriesAfter != 1 || quarantinedAfter != 1 || unprocessed(t, p) != 0 {
		t.Fatalf("after quarantined redelivery: entries=%d quarantined=%d unprocessed=%d", entriesAfter, quarantinedAfter, unprocessed(t, p))
	}
	var invalidRockets int
	if err := p.QueryRow(ctx, `select count(*) from rockets where channel = 'bad/channel'`).Scan(&invalidRockets); err != nil {
		t.Fatal(err)
	}
	if invalidRockets != 0 {
		t.Fatalf("invalid channel created %d rockets", invalidRockets)
	}
	got := testdb.LoadRocket(t, p, validChannel)
	if got.Speed != 800 || got.RocketType == nil || *got.RocketType != "A" {
		t.Fatalf("valid out-of-order rocket = %+v, want launched at speed 800", got)
	}
}
