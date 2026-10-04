package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// A Handler performs one attempt of one kind of job. Returning nil marks the
// job succeeded. Returning a *WaitError parks it (it stays alive and resumes
// later); a *FatalError ends it for good; anything else is treated as
// transient and retried with backoff until the job's attempts run out.
//
// A handler must be safe to call again: this is the promise the whole
// runner rests on, because a restart, a timeout and a retry all ask for
// exactly that. Whatever it accomplishes durably belongs in the store
// (checkpoints, evidence, resume state), never only in its return value.
type Handler func(ctx context.Context, j Job) error

// WaitError says the job is not failing — it is blocked on something
// outside itself, and the owner is that something more often than not.
// Waiting work survives restarts; it is never turned into a failure by
// waiting long enough.
type WaitError struct {
	Status Status // StatusWaitingPermission or StatusWaitingUser
	Reason string
}

func (e *WaitError) Error() string {
	if e == nil || e.Reason == "" {
		return "waiting"
	}
	return e.Reason
}

// WaitingPermission is the approval checkpoint: the handler reached
// something consequential and stopped for the owner instead of deciding
// alone. The job parks in waiting_for_permission and is started again,
// from its own resume state, once the grant lands.
func WaitingPermission(reason string) error {
	return &WaitError{Status: StatusWaitingPermission, Reason: reason}
}

// WaitingUser parks the job until the owner says something back.
func WaitingUser(reason string) error {
	return &WaitError{Status: StatusWaitingUser, Reason: reason}
}

// Waiting reports whether err asks the job to park rather than fail.
func Waiting(err error) (*WaitError, bool) {
	var w *WaitError
	if errors.As(err, &w) {
		return w, true
	}
	return nil, false
}

// FatalError marks a failure no further attempt can fix — a task the owner
// cancelled mid-run, a credential that was revoked, an argument that will
// be just as wrong next time. Retrying it would burn the budget on an
// answer the runner already knows.
type FatalError struct{ Reason string }

func (e *FatalError) Error() string {
	if e == nil || e.Reason == "" {
		return "failed"
	}
	return e.Reason
}

// Fatal wraps reason as a non-retryable failure.
func Fatal(reason string) error { return &FatalError{Reason: reason} }

// FatalErrorOf reports err as non-retryable when it is one.
func FatalErrorOf(err error) (*FatalError, bool) {
	var f *FatalError
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// BusyError says the job could not start right now for a reason that is
// not its own — a concurrency slot was full, a rate limit asked us to
// come back. The runner waits and tries again without charging an
// attempt: a busy moment must never be able to kill work that never
// failed, no matter how many busy moments there are.
type BusyError struct {
	// Delay is how long to wait. Zero means the runner picks a backoff.
	Delay  time.Duration
	Reason string
}

func (e *BusyError) Error() string {
	if e == nil || e.Reason == "" {
		return "not now"
	}
	return e.Reason
}

// Busy builds a not-now error with an explicit wait.
func Busy(delay time.Duration, reason string) error {
	return &BusyError{Delay: delay, Reason: reason}
}

// AsBusy reports err as a not-now outcome.
func AsBusy(err error) (*BusyError, bool) {
	var b *BusyError
	if errors.As(err, &b) {
		return b, true
	}
	return nil, false
}

// Runner claims due jobs and runs them, one attempt at a time, until they
// succeed, park, or run out of attempts. It is the half of the durable job
// store that actually does the work: the store records what is true, the
// runner makes it true again after a process that stopped in the middle.
type Runner struct {
	store *Store

	mu       sync.Mutex
	handlers map[string]Handler
	// inFlight owns a job while an attempt is running, keyed by id. It is
	// what makes a second tick in the same instant a no-op rather than a
	// second execution of somebody's work.
	inFlight map[string]context.CancelFunc
	started  bool
	stopped  bool
	// recovered records that crash leftovers have been requeued. It is not
	// set until Recover actually succeeds: this runtime starts the runner
	// from inside the agent loop, before the schema migration runs, so the
	// first attempts can fail on a jobs table that lacks the retry columns
	// yet — and a recovery that gave up there would strand exactly the
	// work it exists to save.
	recovered bool
	baseCtx   context.Context
	cancel    context.CancelFunc

	wake  chan struct{}
	loops sync.WaitGroup
	wg    sync.WaitGroup

	backoffBase time.Duration
	backoffMax  time.Duration
	maxAttempts int
	poll        time.Duration
	batch       int

	onSettled func(Job)
	logf      func(format string, args ...interface{})
}

// NewRunner builds a runner over a store. It claims nothing and runs
// nothing until Start or Tick is called.
func NewRunner(store *Store) *Runner {
	return &Runner{
		store:       store,
		handlers:    map[string]Handler{},
		inFlight:    map[string]context.CancelFunc{},
		wake:        make(chan struct{}, 1),
		backoffBase: 5 * time.Second,
		backoffMax:  15 * time.Minute,
		maxAttempts: 4,
		poll:        2 * time.Second,
		batch:       8,
		logf:        func(string, ...interface{}) {},
	}
}

// Register binds a job kind to the handler that runs it. Kinds with no
// handler are never claimed: a job nobody can execute must stay readable
// as pending rather than be run by whatever happens to be nearby.
func (r *Runner) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

// HandlerFor returns the handler registered for a kind, if any.
func (r *Runner) HandlerFor(kind string) (Handler, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handlers[kind]
	return h, ok
}

// SetBackoff sets the delay before the first retry and the ceiling it grows
// to. Delays double per failure, so a service that is down for a minute
// costs about a minute instead of eight immediate refusals.
func (r *Runner) SetBackoff(base, max time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if base > 0 {
		r.backoffBase = base
	}
	if max >= base {
		r.backoffMax = max
	}
}

// SetDefaultMaxAttempts sets the retry budget for jobs that do not name
// their own. Zero means unlimited — a choice, made for work whose value is
// in finishing rather than in never trying twice.
func (r *Runner) SetDefaultMaxAttempts(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxAttempts = n
}

// SetPollInterval sets how often the runner looks for due work.
func (r *Runner) SetPollInterval(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d > 0 {
		r.poll = d
	}
}

// SetLogf installs a diagnostic sink. Nil-safe.
func (r *Runner) SetLogf(fn func(string, ...interface{})) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fn != nil {
		r.logf = fn
	}
}

// SetSettleHook installs the callback invoked after every attempt reaches a
// state — succeeded, failed, waiting, or queued for another try. It is how
// results get to the owner. It runs on the attempt's own goroutine, so it
// must not block: the runner is not a delivery mechanism.
func (r *Runner) SetSettleHook(fn func(Job)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onSettled = fn
}

// Start runs the claim loop until ctx is cancelled or Stop is called.
// Calling it twice is harmless; the second call does nothing.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.baseCtx, r.cancel = context.WithCancel(ctx)
	poll := r.poll
	r.mu.Unlock()

	r.loops.Add(1)
	go func() {
		defer r.loops.Done()
		t := time.NewTicker(poll)
		defer t.Stop()
		for {
			r.Tick(r.baseCtx)
			select {
			case <-r.baseCtx.Done():
				return
			case <-t.C:
			case <-r.wake:
			}
		}
	}()
}

// Wake asks the loop to look for work now instead of at the next tick. It
// is what makes a fresh spawn run immediately rather than a poll later.
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Stop halts the loop and cancels attempts in flight. Interrupted jobs are
// not failed here: they are left running, to be marked interrupted by the
// next boot's MarkInterrupted, which is the only place that can tell a
// crash from a stop.
func (r *Runner) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	cancel := r.cancel
	pending := make([]context.CancelFunc, 0, len(r.inFlight))
	for _, c := range r.inFlight {
		pending = append(pending, c)
	}
	r.mu.Unlock()

	// Stop the claim loop first: after this it can neither dispatch new
	// work nor add to the counter Wait is about to observe.
	if cancel != nil {
		cancel()
	}
	r.Wake()
	r.loops.Wait()

	for _, c := range pending {
		c()
	}
	r.wg.Wait()
}

// Tick claims one batch of due jobs and starts each in its own goroutine.
// It returns as soon as the work is dispatched; Wait blocks for it.
func (r *Runner) Tick(ctx context.Context) {
	r.ensureRecovered()
	due, err := r.store.ListDue(r.batch)
	if err != nil {
		r.logf("tasks: list due: %v", err)
		return
	}
	for _, j := range due {
		r.dispatch(ctx, j)
	}
}

// ensureRecovered runs the one-time requeue of crash leftovers, and keeps
// trying until it actually succeeds rather than once at startup. The agent
// loop — and therefore this runner — is built before the schema migration
// runs, so the first attempt can fail on a jobs table that does not yet
// have the retry columns. A recovery that gave up on that first error
// would leave interrupted work stranded until the next restart.
func (r *Runner) ensureRecovered() {
	r.mu.Lock()
	done := r.recovered
	r.mu.Unlock()
	if done {
		return
	}
	if _, err := r.Recover(); err != nil {
		r.logf("tasks: recovery not ready yet: %v", err)
	}
}

// dispatch claims one job for one attempt. The job is recorded as in flight
// BEFORE the state change, so two ticks racing on the same row cannot both
// decide they won: whoever gets there second sees it taken and leaves.
func (r *Runner) dispatch(ctx context.Context, j Job) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	if _, busy := r.inFlight[j.ID]; busy {
		r.mu.Unlock()
		return
	}
	h, ok := r.handlers[j.Kind]
	if !ok {
		r.mu.Unlock()
		r.logf("tasks: no handler for kind %q (job %s stays pending)", j.Kind, j.ID)
		return
	}
	actx, cancel := context.WithCancel(ctx)
	r.inFlight[j.ID] = cancel
	r.mu.Unlock()

	release := func() {
		cancel()
		r.mu.Lock()
		delete(r.inFlight, j.ID)
		r.mu.Unlock()
	}

	started, err := r.store.Start(j.ID)
	if err != nil || started.Status != StatusRunning {
		// Lost the row to a cancel, a pause or another claim: not ours.
		r.logf("tasks: not claiming %s: status=%s err=%v", j.ID, started.Status, err)
		release()
		return
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer release()
		r.execute(actx, h, started)
	}()
}

// execute runs one attempt and settles the job on its outcome. A panicking
// handler is a failed attempt, not a dead runner: work that crashes the
// process it runs in must still be recorded as failing so the budget, and
// not the crash, is what stops it.
func (r *Runner) execute(ctx context.Context, h Handler, j Job) {
	err := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("handler panicked: %v", rec)
			}
		}()
		return h(ctx, j)
	}()
	// An attempt that was stopped is not an attempt that failed. Leave the
	// job interrupted with a rotated generation: the next boot resumes it,
	// and a runner being shut down never eats into the work's own budget.
	if err != nil && ctx.Err() != nil {
		if ierr := r.store.InterruptOne(j.ID); ierr != nil {
			r.logf("tasks: interrupt %s: %v", j.ID, ierr)
		}
		return
	}
	r.settle(j, err)
}

// settle turns an attempt's outcome into the job's next state.
func (r *Runner) settle(j Job, err error) {
	switch {
	case err == nil:
		r.finish(r.store.Succeed(j.ID))
	default:
		if w, ok := Waiting(err); ok {
			st := w.Status
			if st != StatusWaitingPermission && st != StatusWaitingUser {
				st = StatusWaitingUser
			}
			r.finish(r.store.SetWaiting(j.ID, st, w.Reason))
			return
		}
		if f, ok := FatalErrorOf(err); ok {
			r.finish(r.store.Fail(j.ID, f.Reason))
			return
		}
		if b, ok := AsBusy(err); ok {
			delay := b.Delay
			if delay <= 0 {
				delay = r.backoffFor(1)
			}
			if _, serr := r.store.Snooze(j.ID, delay, b.Reason); serr != nil {
				r.logf("tasks: snooze %s: %v", j.ID, serr)
				return
			}
			next, _ := r.store.Get(j.ID)
			r.settled(next)
			return
		}
		// Transient. Read the job back before deciding: another actor may
		// have raised the ceiling or cancelled it while we were working.
		cur, gerr := r.store.Get(j.ID)
		if gerr != nil {
			r.logf("tasks: read %s after failure: %v", j.ID, gerr)
			return
		}
		if r.exhausted(cur) {
			r.finish(r.store.Fail(j.ID, err.Error()))
			return
		}
		delay := r.backoffFor(cur.Failures + 1)
		if _, serr := r.store.ScheduleRetry(j.ID, delay, err.Error()); serr != nil {
			r.logf("tasks: schedule retry for %s: %v", j.ID, serr)
			return
		}
		next, _ := r.store.Get(j.ID)
		r.settled(next)
	}
}

// exhausted reports whether the job has spent its retry budget.
func (r *Runner) exhausted(j Job) bool {
	r.mu.Lock()
	dflt := r.maxAttempts
	r.mu.Unlock()
	max := j.MaxAttempts
	if max <= 0 {
		max = dflt
	}
	return max > 0 && j.Failures >= max
}

// backoffFor doubles the wait per failure: 5s, 10s, 20s... capped, so a job
// that keeps failing stays worth another try without hammering anything.
func (r *Runner) backoffFor(failures int) time.Duration {
	r.mu.Lock()
	base, max := r.backoffBase, r.backoffMax
	r.mu.Unlock()
	if failures < 1 {
		failures = 1
	}
	d := base
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

// finish reports a job that reached a state worth telling the owner about.
func (r *Runner) finish(j Job, err error) {
	if err != nil {
		r.logf("tasks: settle %s: %v", j.ID, err)
		return
	}
	r.settled(j)
}

func (r *Runner) settled(j Job) {
	r.mu.Lock()
	hook := r.onSettled
	r.mu.Unlock()
	if hook != nil && j.ID != "" {
		hook(j)
	}
}

// Recover re-queues work a crash left behind. It runs after
// MarkInterrupted, which has already flagged running jobs as interrupted
// and rotated their generations.
//
// An interruption counts against the retry budget on purpose. Most restarts
// are a machine rebooting, and four of those still fit inside the budget;
// a job that takes the process down with it must not be able to try
// forever. Work with no handler is left alone — an interrupted job nobody
// can execute should stay readable, not be failed for a reason that is not
// its own.
func (r *Runner) Recover() (int, error) {
	jobs, err := r.store.List(StatusInterrupted)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted: %w", err)
	}
	requeued := 0
	for _, j := range jobs {
		if _, ok := r.HandlerFor(j.Kind); !ok {
			continue
		}
		if r.exhausted(j) {
			r.finish(r.store.Fail(j.ID, fmt.Sprintf("gave up after %d interrupted attempts: %s", j.Failures, j.Error)))
			continue
		}
		if _, err := r.store.ScheduleRetry(j.ID, 0, "requeued after restart"); err != nil {
			r.logf("tasks: requeue %s: %v", j.ID, err)
			continue
		}
		requeued++
	}
	r.mu.Lock()
	r.recovered = true
	r.mu.Unlock()
	return requeued, nil
}

// Wait blocks until every dispatched attempt has settled. Tests use it to
// observe a Tick's work; production lets the loop tick again underneath.
func (r *Runner) Wait() { r.wg.Wait() }

// InFlight reports how many attempts are running right now.
func (r *Runner) InFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inFlight)
}
