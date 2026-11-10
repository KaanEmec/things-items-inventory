package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

func household(t *testing.T, d *db.DB) string {
	t.Helper()
	hid := id.New()
	now := fmtTime(time.Now())
	_, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
		ID: hid, Name: "h", Language: "en", Currency: "EUR", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return hid
}

func fastOpts() Options {
	return Options{
		PollInterval: 20 * time.Millisecond,
		Backoff:      []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
		StopGrace:    50 * time.Millisecond,
	}
}

func start(t *testing.T, q *Queue) {
	t.Helper()
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		q.Stop(ctx)
	})
}

func waitState(t *testing.T, q *Queue, hid, jid string, states ...string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, err := q.Get(context.Background(), hid, jid)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range states {
			if j.State == s {
				return j
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s stuck in %s (last_error %q), want %v", jid, j.State, j.LastError, states)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunsOnce(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	var runs atomic.Int32
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		runs.Add(1)
		return json.RawMessage(`{"ok":true}`), nil
	}})
	start(t, q)
	jid, err := q.Enqueue(context.Background(), d.Write, Job{HouseholdID: hid, Kind: "k", Payload: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, q, hid, jid, StateDone)
	time.Sleep(60 * time.Millisecond)
	if runs.Load() != 1 || j.Attempts != 1 || string(j.Result) != `{"ok":true}` || j.FinishedAt == nil {
		t.Fatalf("runs=%d job=%+v", runs.Load(), j)
	}
}

func TestEnqueueInTransaction(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	var jid string
	rollback := errors.New("rollback")
	err := d.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		jid, err = q.Enqueue(context.Background(), tx, Job{HouseholdID: hid, Kind: "k"})
		if err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := q.Get(context.Background(), hid, jid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back job visible: %v", err)
	}
}

func TestNoDoubleClaim(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	const n = 100
	var mu sync.Mutex
	ran := map[string]int{}
	var cur, peak atomic.Int32
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		c := cur.Add(1)
		for {
			p := peak.Load()
			if c <= p || peak.CompareAndSwap(p, c) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		cur.Add(-1)
		mu.Lock()
		ran[j.ID]++
		mu.Unlock()
		return nil, nil
	}})
	ids := make([]string, n)
	for i := range ids {
		var err error
		ids[i], err = q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
		if err != nil {
			t.Fatal(err)
		}
	}
	start(t, q)
	for _, jid := range ids {
		waitState(t, q, hid, jid, StateDone)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, jid := range ids {
		if ran[jid] != 1 {
			t.Fatalf("job %s ran %d times", jid, ran[jid])
		}
	}
	if peak.Load() != 3 {
		t.Fatalf("max concurrency %d, want 3", peak.Load())
	}
}

func TestRetryThenSuccess(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	var calls atomic.Int32
	q.Register("k", Handler{MaxAttempts: 4, Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("503 upstream")
		}
		return nil, nil
	}})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	j := waitState(t, q, hid, jid, StateDone)
	if j.Attempts != 3 || j.LastError != "" {
		t.Fatalf("attempts=%d last_error=%q", j.Attempts, j.LastError)
	}
}

type codedErr struct{}

func (codedErr) Error() string     { return "bad token" }
func (codedErr) ErrorCode() string { return "invalid_token" }

func TestPermanentFailure(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	var calls atomic.Int32
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		calls.Add(1)
		return nil, Permanent(codedErr{})
	}})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	j := waitState(t, q, hid, jid, StateFailed)
	if calls.Load() != 1 || j.LastError != "invalid_token: bad token" {
		t.Fatalf("calls=%d last_error=%q", calls.Load(), j.LastError)
	}
}

func TestAttemptsExhaustedAndRetry(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	var fail atomic.Bool
	fail.Store(true)
	q.Register("k", Handler{MaxAttempts: 3, Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		if fail.Load() {
			return nil, errors.New("model overloaded")
		}
		return nil, nil
	}})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	j := waitState(t, q, hid, jid, StateFailed)
	if j.Attempts != 3 || !strings.Contains(j.LastError, "gave up after 3 attempts") || !strings.Contains(j.LastError, "model overloaded") {
		t.Fatalf("attempts=%d last_error=%q", j.Attempts, j.LastError)
	}
	if _, err := q.Retry(context.Background(), hid, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry unknown: %v", err)
	}
	fail.Store(false)
	if _, err := q.Retry(context.Background(), hid, jid); err != nil {
		t.Fatal(err)
	}
	j = waitState(t, q, hid, jid, StateDone)
	if j.Attempts != 1 {
		t.Fatalf("attempts after retry = %d", j.Attempts)
	}
	if _, err := q.Retry(context.Background(), hid, jid); !errors.Is(err, ErrWrongState) {
		t.Fatalf("retry of done job: %v", err)
	}
}

func TestRestartRecovery(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	// A first process claims the job and dies.
	q1 := New(d, fastOpts())
	jid, _ := q1.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	if _, ok := func() (Job, bool) { q1.rootCtx = context.Background(); return q1.claim() }(); !ok {
		t.Fatal("claim failed")
	}
	if j, _ := q1.Get(context.Background(), hid, jid); j.State != StateRunning {
		t.Fatalf("state %s", j.State)
	}

	q2 := New(d, fastOpts())
	var runs atomic.Int32
	q2.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		runs.Add(1)
		return nil, nil
	}})
	start(t, q2)
	waitState(t, q2, hid, jid, StateDone)
	if runs.Load() != 1 {
		t.Fatalf("runs=%d", runs.Load())
	}
}

func TestGracefulStopRequeues(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	started := make(chan struct{})
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := q.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := q.Get(context.Background(), hid, jid)
	if j.State != StateQueued || j.Attempts != 0 {
		t.Fatalf("state=%s attempts=%d", j.State, j.Attempts)
	}
}

func TestStopReleasesStuckHandler(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	started := make(chan struct{})
	unblock := make(chan struct{})
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		close(started)
		<-unblock // ignores ctx
		return nil, nil
	}})
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := q.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v", err)
	}
	j, _ := q.Get(context.Background(), hid, jid)
	if j.State != StateQueued || j.Attempts != 0 {
		t.Fatalf("state=%s attempts=%d", j.State, j.Attempts)
	}
	close(unblock)
	time.Sleep(50 * time.Millisecond)
	if j, _ = q.Get(context.Background(), hid, jid); j.State != StateQueued {
		t.Fatalf("late finish overwrote state: %s", j.State)
	}
}

func TestFairnessAcrossHouseholds(t *testing.T) {
	d := dbtest.New(t)
	a, b := household(t, d), household(t, d)
	var tick atomic.Int64
	base := time.Now().Add(-time.Hour)
	opts := fastOpts()
	opts.Workers = 1
	opts.Now = func() time.Time { return base.Add(time.Duration(tick.Add(1)) * time.Millisecond) }
	q := New(d, opts)
	var mu sync.Mutex
	var order []string
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		mu.Lock()
		order = append(order, string(j.Payload))
		mu.Unlock()
		return nil, nil
	}})
	for _, e := range []struct{ h, p string }{
		{a, `"a1"`}, {a, `"a2"`}, {a, `"a3"`}, {b, `"b1"`}, {b, `"b2"`}, {b, `"b3"`},
	} {
		if _, err := q.Enqueue(context.Background(), nil, Job{HouseholdID: e.h, Kind: "k", Payload: json.RawMessage(e.p)}); err != nil {
			t.Fatal(err)
		}
	}
	start(t, q)
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n == 6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := strings.Join(order, ",")
	mu.Unlock()
	if want := `"b3","a3","b2","a2","b1","a1"`; got != want {
		t.Fatalf("order %s, want %s", got, want)
	}
}

func TestCancel(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	started := make(chan struct{})
	var ran atomic.Int32
	q.Register("slow", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		ran.Add(1)
		return nil, nil
	}})
	// A queued job (not yet due) is cancelled before it runs.
	later, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k", RunAfter: time.Now().Add(time.Hour)})
	if j, err := q.Cancel(context.Background(), hid, later); err != nil || j.State != StateCancelled {
		t.Fatalf("cancel queued: %v %+v", err, j)
	}
	if _, err := q.Cancel(context.Background(), hid, later); !errors.Is(err, ErrWrongState) {
		t.Fatalf("second cancel: %v", err)
	}
	if _, err := q.Cancel(context.Background(), household(t, d), later); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other household: %v", err)
	}
	// A running job has its context cancelled and stays cancelled.
	start(t, q)
	run, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "slow"})
	<-started
	if _, err := q.Cancel(context.Background(), hid, run); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if j, _ := q.Get(context.Background(), hid, run); j.State != StateCancelled {
		t.Fatalf("state %s", j.State)
	}
	if ran.Load() != 0 {
		t.Fatal("cancelled job ran")
	}
}

func TestHandlerTimeout(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	q.Register("k", Handler{Timeout: 20 * time.Millisecond, MaxAttempts: 2, Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	j := waitState(t, q, hid, jid, StateFailed)
	if j.Attempts != 2 || !strings.HasPrefix(j.LastError, "timeout: gave up after 2 attempts") {
		t.Fatalf("attempts=%d last_error=%q", j.Attempts, j.LastError)
	}
}

func TestPanicFailsJobNotProcess(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	q.Register("boom", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) {
		panic("kaboom")
	}})
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) { return nil, nil }})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "boom"})
	j := waitState(t, q, hid, jid, StateFailed)
	if !strings.HasPrefix(j.LastError, "panic: panic: kaboom") {
		t.Fatalf("last_error=%q", j.LastError)
	}
	// The pool still works.
	ok, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	waitState(t, q, hid, ok, StateDone)
}

func TestUnknownKindFails(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	q := New(d, fastOpts())
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "nope"})
	j := waitState(t, q, hid, jid, StateFailed)
	if !strings.HasPrefix(j.LastError, "no_handler:") {
		t.Fatalf("last_error=%q", j.LastError)
	}
}

func TestCleanupAndOnChange(t *testing.T) {
	d := dbtest.New(t)
	hid := household(t, d)
	var mu sync.Mutex
	var states []string
	opts := fastOpts()
	opts.OnChange = func(j Job) { mu.Lock(); states = append(states, j.State); mu.Unlock() }
	opts.Retention = 50 * time.Millisecond
	opts.CleanupInterval = 30 * time.Millisecond
	q := New(d, opts)
	q.Register("k", Handler{Run: func(ctx context.Context, j Job) (json.RawMessage, error) { return nil, nil }})
	start(t, q)
	jid, _ := q.Enqueue(context.Background(), nil, Job{HouseholdID: hid, Kind: "k"})
	waitState(t, q, hid, jid, StateDone)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := q.Get(context.Background(), hid, jid); errors.Is(err, ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old job not cleaned up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(states, ","); got != "queued,running,done" {
		t.Fatalf("OnChange states %s", got)
	}
}
