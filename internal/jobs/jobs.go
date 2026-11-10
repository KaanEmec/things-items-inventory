// Package jobs is the background job queue: a jobs table plus a small worker
// pool inside the one binary.
//
// A feature registers a handler for its kind, then enqueues jobs, usually in
// the same transaction as the change that needs them:
//
//	q.Register("draft_item", jobs.Handler{Run: run, Timeout: time.Minute, MaxAttempts: 4})
//	err := d.Tx(ctx, func(tx *sql.Tx) error {
//		// ... change rows ...
//		_, err := q.Enqueue(ctx, tx, jobs.Job{HouseholdID: h, Kind: "draft_item", Payload: p})
//		return err
//	})
//
// Handlers must be idempotent: a job can run again after a crash.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Job states.
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

var (
	ErrNotFound     = errors.New("jobs: job not found")
	ErrWrongState   = errors.New("jobs: job is not in a state that allows this")
	errShutdown     = errors.New("jobs: shutting down")
	errCancelledJob = errors.New("jobs: cancelled")
)

const timeFormat = "2006-01-02T15:04:05.000Z"

// Job is one row of the jobs table. For Enqueue, only HouseholdID, Kind,
// Payload, CreatedBy, ItemID, PlaceID and RunAfter are used.
type Job struct {
	ID          string
	HouseholdID string
	Kind        string
	Payload     json.RawMessage // JSON; empty means {}
	State       string
	Attempts    int
	RunAfter    time.Time // zero on Enqueue means now
	LastError   string
	Result      json.RawMessage
	CreatedBy   string // user id; empty for none
	ItemID      string // subject, for queue display
	PlaceID     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

// Handler runs one job kind.
type Handler struct {
	// Run does the work. A nil result is stored as NULL.
	Run func(ctx context.Context, job Job) (result json.RawMessage, err error)
	// Timeout bounds one attempt (default 5 minutes).
	Timeout time.Duration
	// MaxAttempts is the total number of tries (default 5).
	MaxAttempts int
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks err as not worth retrying: the job fails at once.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// Coded can be implemented by handler errors to give last_error a typed code
// (for example "invalid_token"). Without it the code is "error".
type Coded interface{ ErrorCode() string }

// Options configure a Queue. Zero values mean the defaults.
type Options struct {
	Workers         int             // default 3
	PollInterval    time.Duration   // idle fallback poll, default 10s
	Backoff         []time.Duration // retry delays, default 5s, 30s, 2m, 10m
	StopGrace       time.Duration   // time running handlers get on Stop, default 5s
	Retention       time.Duration   // finished jobs kept, default 30 days
	CleanupInterval time.Duration   // default 24h
	Now             func() time.Time
	// OnChange is called after every state change (queued, running, done,
	// failed, cancelled, back to queued). Enqueue inside a caller's *sql.Tx
	// does not call it, since the commit is not known: the caller reports it.
	// It must not block.
	OnChange func(Job)
}

// Queue stores jobs and runs them.
type Queue struct {
	db   *db.DB
	opts Options

	mu       sync.RWMutex
	handlers map[string]Handler

	nudge    chan struct{}
	stopping chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	runMu   sync.Mutex
	running map[string]context.CancelCauseFunc // job id -> cancel
	rootCtx context.Context
	cancel  context.CancelCauseFunc
	started bool
}

// New returns a Queue; call Register for each kind, then Start.
func New(d *db.DB, opts Options) *Queue {
	if opts.Workers <= 0 {
		opts.Workers = 3
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 10 * time.Second
	}
	if len(opts.Backoff) == 0 {
		opts.Backoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	}
	if opts.StopGrace <= 0 {
		opts.StopGrace = 5 * time.Second
	}
	if opts.Retention <= 0 {
		opts.Retention = 30 * 24 * time.Hour
	}
	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = 24 * time.Hour
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Queue{
		db:       d,
		opts:     opts,
		handlers: map[string]Handler{},
		nudge:    make(chan struct{}, 1),
		stopping: make(chan struct{}),
		running:  map[string]context.CancelCauseFunc{},
	}
}

// Register sets the handler for a kind. It panics on a duplicate kind or a
// nil Run: both are programming errors.
func (q *Queue) Register(kind string, h Handler) {
	if kind == "" || h.Run == nil {
		panic("jobs: Register needs a kind and a Run function")
	}
	if h.Timeout <= 0 {
		h.Timeout = 5 * time.Minute
	}
	if h.MaxAttempts <= 0 {
		h.MaxAttempts = 5
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, dup := q.handlers[kind]; dup {
		panic("jobs: kind registered twice: " + kind)
	}
	q.handlers[kind] = h
}

func (q *Queue) now() time.Time { return q.opts.Now().UTC() }

func fmtTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullTime(t time.Time) sql.NullString { return sql.NullString{String: fmtTime(t), Valid: true} }

func parseTime(s string) time.Time {
	t, _ := time.Parse(timeFormat, s)
	return t
}

func fromRow(r sqlc.Job) Job {
	j := Job{
		ID: r.ID, HouseholdID: r.HouseholdID, Kind: r.Kind,
		Payload: json.RawMessage(r.Payload), State: r.State, Attempts: int(r.Attempts),
		RunAfter: parseTime(r.RunAfter), LastError: r.LastError.String,
		CreatedBy: r.CreatedBy.String, ItemID: r.ItemID.String, PlaceID: r.PlaceID.String,
		CreatedAt: parseTime(r.CreatedAt), UpdatedAt: parseTime(r.UpdatedAt),
	}
	if r.Result.Valid {
		j.Result = json.RawMessage(r.Result.String)
	}
	if r.StartedAt.Valid {
		t := parseTime(r.StartedAt.String)
		j.StartedAt = &t
	}
	if r.FinishedAt.Valid {
		t := parseTime(r.FinishedAt.String)
		j.FinishedAt = &t
	}
	return j
}

func (q *Queue) changed(r sqlc.Job) {
	if q.opts.OnChange != nil {
		q.opts.OnChange(fromRow(r))
	}
}

// Enqueue inserts a queued job and returns its id. x is a *sql.Tx to enqueue
// in the caller's transaction (the job exists only if it commits), or a
// *sql.DB (nil means the write connection) for a standalone insert.
func (q *Queue) Enqueue(ctx context.Context, x sqlc.DBTX, j Job) (string, error) {
	if j.HouseholdID == "" || j.Kind == "" {
		return "", errors.New("jobs: Enqueue needs HouseholdID and Kind")
	}
	payload := j.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return "", errors.New("jobs: payload is not valid JSON")
	}
	if x == nil {
		x = q.db.Write
	}
	now := q.now()
	runAfter := j.RunAfter
	if runAfter.IsZero() {
		runAfter = now
	}
	row, err := sqlc.New(x).InsertJob(ctx, sqlc.InsertJobParams{
		ID: id.New(), HouseholdID: j.HouseholdID,
		CreatedAt: fmtTime(now), UpdatedAt: fmtTime(now),
		Kind: j.Kind, Payload: string(payload), RunAfter: fmtTime(runAfter),
		CreatedBy: nullStr(j.CreatedBy), ItemID: nullStr(j.ItemID), PlaceID: nullStr(j.PlaceID),
	})
	if err != nil {
		return "", fmt.Errorf("jobs: enqueue: %w", err)
	}
	// Waking a worker before the caller's commit is harmless: the claim waits
	// for the single write connection, which the open transaction holds.
	q.wake()
	if d := runAfter.Sub(now); d > 0 {
		time.AfterFunc(d, q.wake)
	}
	if _, inTx := x.(*sql.Tx); !inTx {
		q.changed(row)
	}
	return row.ID, nil
}

func (q *Queue) wake() {
	select {
	case q.nudge <- struct{}{}:
	default:
	}
}

// Get returns a job of the household.
func (q *Queue) Get(ctx context.Context, householdID, jobID string) (Job, error) {
	row, err := sqlc.New(q.db.Read).GetJob(ctx, sqlc.GetJobParams{HouseholdID: householdID, ID: jobID})
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	return fromRow(row), nil
}

// Cancel cancels a queued or running job (a running handler's context is
// cancelled). It returns ErrWrongState for a finished job.
func (q *Queue) Cancel(ctx context.Context, householdID, jobID string) (Job, error) {
	row, err := sqlc.New(q.db.Write).CancelJob(ctx, sqlc.CancelJobParams{
		Now: nullTime(q.now()), HouseholdID: householdID, ID: jobID})
	if err != nil {
		return Job{}, q.explain(ctx, householdID, jobID, err)
	}
	q.runMu.Lock()
	cancel := q.running[jobID]
	q.runMu.Unlock()
	if cancel != nil {
		cancel(errCancelledJob)
	}
	q.changed(row)
	return fromRow(row), nil
}

// Retry puts a failed job back in the queue with its attempts reset.
func (q *Queue) Retry(ctx context.Context, householdID, jobID string) (Job, error) {
	row, err := sqlc.New(q.db.Write).RetryJob(ctx, sqlc.RetryJobParams{
		Now: fmtTime(q.now()), HouseholdID: householdID, ID: jobID})
	if err != nil {
		return Job{}, q.explain(ctx, householdID, jobID, err)
	}
	q.wake()
	q.changed(row)
	return fromRow(row), nil
}

// explain turns "no rows" into ErrNotFound or ErrWrongState.
func (q *Queue) explain(ctx context.Context, householdID, jobID string, err error) error {
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, gerr := q.Get(ctx, householdID, jobID); gerr != nil {
		return gerr
	}
	return ErrWrongState
}

// Start recovers jobs a dead process left running, then starts the workers
// and the daily cleanup. Register all kinds first. Handler contexts end when
// ctx ends or Stop gives up waiting.
func (q *Queue) Start(ctx context.Context) error {
	q.runMu.Lock()
	if q.started {
		q.runMu.Unlock()
		return errors.New("jobs: already started")
	}
	q.started = true
	q.rootCtx, q.cancel = context.WithCancelCause(ctx)
	q.runMu.Unlock()

	rows, err := sqlc.New(q.db.Write).RequeueRunningJobs(ctx, fmtTime(q.now()))
	if err != nil {
		return fmt.Errorf("jobs: recover running jobs: %w", err)
	}
	for _, r := range rows {
		q.changed(r)
	}
	for i := 0; i < q.opts.Workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	q.wg.Add(1)
	go q.cleaner()
	return nil
}

// Stop stops claiming, gives running handlers StopGrace to finish, cancels
// them, and puts any unfinished job back to queued without counting the
// attempt. It returns ctx.Err() if handlers still ran when ctx ended.
func (q *Queue) Stop(ctx context.Context) error {
	q.stopOnce.Do(func() { close(q.stopping) })
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()

	grace := time.NewTimer(q.opts.StopGrace)
	defer grace.Stop()
	select {
	case <-done:
		return nil
	case <-grace.C:
	case <-ctx.Done():
	}
	if q.cancel != nil {
		q.cancel(errShutdown)
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	// A handler ignores cancellation: release its job here. The worker's own
	// final write is guarded by state = 'running' and becomes a no-op.
	q.runMu.Lock()
	ids := make([]string, 0, len(q.running))
	for id := range q.running {
		ids = append(ids, id)
	}
	q.runMu.Unlock()
	rctx := context.WithoutCancel(ctx)
	for _, jid := range ids {
		q.release(rctx, jid)
	}
	return ctx.Err()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	poll := time.NewTimer(q.opts.PollInterval)
	defer poll.Stop()
	for {
		select {
		case <-q.stopping:
			return
		case <-q.rootCtx.Done():
			return
		default:
		}
		job, ok := q.claim()
		if ok {
			q.wake() // let a sibling look for more work
			q.run(job)
			continue
		}
		if !poll.Stop() {
			select {
			case <-poll.C:
			default:
			}
		}
		poll.Reset(q.opts.PollInterval)
		select {
		case <-q.stopping:
			return
		case <-q.rootCtx.Done():
			return
		case <-q.nudge:
		case <-poll.C:
		}
	}
}

func (q *Queue) claim() (Job, bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(q.rootCtx), 30*time.Second)
	defer cancel()
	row, err := sqlc.New(q.db.Write).ClaimJob(ctx, nullTime(q.now()))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false
	}
	if err != nil {
		select {
		case <-q.stopping:
		default:
			slog.Error("jobs: claim", "err", err)
			time.Sleep(time.Second) // avoid a hot loop on a broken database
		}
		return Job{}, false
	}
	q.changed(row)
	return fromRow(row), true
}

// run executes one claimed job and records the outcome.
func (q *Queue) run(job Job) {
	q.mu.RLock()
	h, ok := q.handlers[job.Kind]
	q.mu.RUnlock()
	ctx := context.WithoutCancel(q.rootCtx)
	if !ok {
		q.finishFail(ctx, job, "no_handler", "no handler registered for kind "+job.Kind)
		return
	}

	if job.Attempts > h.MaxAttempts {
		// Earlier runs never reported back (the process died): stop a job
		// that keeps crashing the process.
		q.finishFail(ctx, job, "crashed", fmt.Sprintf("gave up after %d attempts; the last run did not finish", h.MaxAttempts))
		return
	}

	jobCtx, cancelJob := context.WithCancelCause(q.rootCtx)
	defer cancelJob(nil)
	q.runMu.Lock()
	q.running[job.ID] = cancelJob
	q.runMu.Unlock()
	defer func() {
		q.runMu.Lock()
		delete(q.running, job.ID)
		q.runMu.Unlock()
	}()
	tctx, cancelT := context.WithTimeout(jobCtx, h.Timeout)
	defer cancelT()

	result, err := safeRun(tctx, h, job)
	cause := context.Cause(tctx)

	switch {
	case err == nil:
		var res sql.NullString
		if len(result) > 0 {
			if !json.Valid(result) {
				q.finishFail(ctx, job, "bad_result", "handler returned invalid JSON")
				return
			}
			res = sql.NullString{String: string(result), Valid: true}
		}
		row, err := sqlc.New(q.db.Write).CompleteJob(ctx, sqlc.CompleteJobParams{
			Result: res, Now: nullTime(q.now()), ID: job.ID})
		q.report(row, err)
	case errors.Is(cause, errCancelledJob):
		// Cancel already wrote the state.
	case errors.Is(cause, errShutdown) || q.rootCtx.Err() != nil:
		// Shutting down (Stop, or the context given to Start ended).
		q.release(ctx, job.ID)
	default:
		code := "error"
		var coded Coded
		switch {
		case errors.Is(tctx.Err(), context.DeadlineExceeded) && errors.Is(cause, context.DeadlineExceeded):
			code = "timeout"
			err = fmt.Errorf("gave up after %s", h.Timeout)
		case errors.As(err, &coded):
			code = coded.ErrorCode()
		case errors.As(err, new(*panicError)):
			code = "panic"
		}
		var perm permanentError
		switch {
		case errors.As(err, &perm) || code == "panic":
			q.finishFail(ctx, job, code, err.Error())
		case job.Attempts >= h.MaxAttempts:
			q.finishFail(ctx, job, code, fmt.Sprintf("gave up after %d attempts: %s", job.Attempts, err))
		default:
			q.delay(ctx, job, code, err.Error())
		}
	}
}

type panicError struct{ v any }

func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.v) }

func safeRun(ctx context.Context, h Handler, job Job) (res json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("jobs: handler panic", "job", job.ID, "kind", job.Kind, "panic", r, "stack", string(debug.Stack()))
			res, err = nil, &panicError{r}
		}
	}()
	return h.Run(ctx, job)
}

func lastError(code, msg string) sql.NullString {
	const max = 1000
	if len(msg) > max {
		msg = msg[:max] + "…"
	}
	return sql.NullString{String: code + ": " + msg, Valid: true}
}

func (q *Queue) report(row sqlc.Job, err error) {
	switch {
	case err == nil:
		q.changed(row)
	case errors.Is(err, sql.ErrNoRows):
		// Cancelled or released by Stop in the meantime.
	default:
		slog.Error("jobs: record outcome", "err", err)
	}
}

func (q *Queue) finishFail(ctx context.Context, job Job, code, msg string) {
	row, err := sqlc.New(q.db.Write).FailJob(ctx, sqlc.FailJobParams{
		LastError: lastError(code, msg), Now: nullTime(q.now()), ID: job.ID})
	q.report(row, err)
}

func (q *Queue) delay(ctx context.Context, job Job, code, msg string) {
	i := job.Attempts - 1
	if i < 0 {
		i = 0
	}
	if i >= len(q.opts.Backoff) {
		i = len(q.opts.Backoff) - 1
	}
	d := q.opts.Backoff[i]
	now := q.now()
	row, err := sqlc.New(q.db.Write).DelayJob(ctx, sqlc.DelayJobParams{
		RunAfter: fmtTime(now.Add(d)), LastError: lastError(code, msg), Now: fmtTime(now), ID: job.ID})
	q.report(row, err)
	if err == nil {
		time.AfterFunc(d, q.wake)
	}
}

func (q *Queue) release(ctx context.Context, jobID string) {
	row, err := sqlc.New(q.db.Write).ReleaseJob(ctx, sqlc.ReleaseJobParams{Now: fmtTime(q.now()), ID: jobID})
	q.report(row, err)
}

// cleaner deletes old finished jobs once at start and then every CleanupInterval.
func (q *Queue) cleaner() {
	defer q.wg.Done()
	t := time.NewTicker(q.opts.CleanupInterval)
	defer t.Stop()
	for {
		q.cleanup()
		select {
		case <-q.stopping:
			return
		case <-t.C:
		}
	}
}

func (q *Queue) cleanup() {
	cutoff := q.now().Add(-q.opts.Retention)
	n, err := sqlc.New(q.db.Write).DeleteFinishedJobsBefore(context.WithoutCancel(q.rootCtx), nullTime(cutoff))
	if err != nil {
		slog.Error("jobs: cleanup", "err", err)
	} else if n > 0 {
		slog.Info("jobs: deleted old jobs", "count", n)
	}
}
