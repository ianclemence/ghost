package tasks

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestRunner builds a runner tuned for tests: it looks for work often
// and waits briefly between attempts, so a retry test costs milliseconds
// instead of the minutes a production backoff would take.
func newTestRunner(t *testing.T, s *Store) *Runner {
	t.Helper()
	r := NewRunner(s)
	r.SetBackoff(time.Second, 4*time.Second)
	r.SetPollInterval(10 * time.Millisecond)
	r.SetLogf(t.Logf)
	t.Cleanup(r.Stop)
	return r
}

// waitJob waits for a job to reach want, failing if it settles anywhere
// else first — a job that ends failed while the test wanted succeeded
// should say why (its recorded error), not time out in silence.
func waitJob(t *testing.T, s *Store, id string, want Status, timeout time.Duration) Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := s.Get(id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if j.Status == want {
			return j
		}
		if terminal(j.Status) {
			t.Fatalf("job settled as %s (wanted %s): %s", j.Status, want, j.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := s.Get(id)
	t.Fatalf("timed out waiting for %s; job is %s, attempts=%d failures=%d err=%q",
		want, j.Status, j.Attempts, j.Failures, j.Error)
	return Job{}
}

// waitIdle waits for the runner to let go of everything it is holding.
func waitIdle(t *testing.T, r *Runner) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner still holds %d job(s)", r.InFlight())
}

// A provider being down for a moment is not a broken task. The attempt
// that failed is recorded, waited out, and tried again — and if it comes
// back the owner never hears about the middle of it.
func TestRunnerRetriesTransientFailureThenSucceeds(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)

	var calls int32
	r.Register("mail", func(ctx context.Context, j Job) error {
		if atomic.AddInt32(&calls, 1) < 3 {
			return errors.New("provider unavailable")
		}
		return s.SetEvidence(j.ID, "sent 2 messages")
	})

	j, err := s.Create("mail", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())

	got := waitJob(t, s, j.ID, StatusSucceeded, 10*time.Second)
	if got.Failures != 2 {
		t.Fatalf("failures = %d, want 2 (one per attempt that came back wrong)", got.Failures)
	}
	if got.Evidence != "sent 2 messages" {
		t.Fatalf("evidence = %q, want the handler's record of what it did", got.Evidence)
	}
	if calls != 3 {
		t.Fatalf("handler ran %d times, want 3", calls)
	}
}

// A job that cannot be fixed by trying again must stop, and must stop for
// a reason the owner can read rather than "it broke" twenty times.
func TestRunnerFailsForGoodWhenAttemptsRunOut(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(2)

	r.Register("dead", func(ctx context.Context, j Job) error {
		return errors.New("credential was revoked")
	})

	j, err := s.Create("dead", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())

	got := waitJob(t, s, j.ID, StatusFailed, 10*time.Second)
	if got.Failures != 2 {
		t.Fatalf("failures = %d, want 2", got.Failures)
	}
	if !strings.Contains(got.Error, "credential was revoked") {
		t.Fatalf("error = %q, want the underlying reason", got.Error)
	}
}

// The approval checkpoint. Reaching something consequential parks the job
// instead of deciding alone — and parking is not failing: the budget it
// would otherwise be spending is the owner's time to answer, and work
// whose owner takes a week to reply is not work that gave up.
func TestRunnerParksForApprovalAndResumesWithoutSpendingAttempts(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(2)

	var calls int32
	r.Register("checkout", func(ctx context.Context, j Job) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			return WaitingPermission("confirm the $240 total with the owner")
		}
		return s.SetEvidence(j.ID, "ordered")
	})

	j, err := s.Create("checkout", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())

	parked := waitJob(t, s, j.ID, StatusWaitingPermission, 10*time.Second)
	if parked.Failures != 0 {
		t.Fatalf("failures = %d while parked; waiting on the owner is not a failure", parked.Failures)
	}
	if !strings.Contains(parked.Evidence, "$240") {
		t.Fatalf("evidence = %q, want the reason it is waiting", parked.Evidence)
	}
	waitIdle(t, r)

	// The owner approves.
	if _, err := s.Resume(j.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	done := waitJob(t, s, j.ID, StatusSucceeded, 10*time.Second)
	if done.Failures != 0 {
		t.Fatalf("failures = %d after an approval cycle, want 0", done.Failures)
	}
	if done.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (started, parked, started again)", done.Attempts)
	}
}

// Work that was running when the process died comes back. The restart
// flags it, the runner re-queues it, and it runs to completion — the whole
// point of the store being durable rather than in memory.
func TestRunnerRecoverRequeuesInterruptedWork(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)

	done := make(chan struct{}, 1)
	r.Register("crawl", func(ctx context.Context, j Job) error {
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})

	j, err := s.Create("crawl", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The process took the job and died before finishing it.
	if _, err := s.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}
	interrupted, _ := s.Get(j.ID)
	if interrupted.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", interrupted.Status)
	}

	n, err := r.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued %d jobs, want 1", n)
	}

	r.Start(context.Background())
	waitJob(t, s, j.ID, StatusSucceeded, 10*time.Second)
	select {
	case <-done:
	default:
		t.Fatal("handler never ran after recovery")
	}
}

// A job that keeps taking the process with it must not be able to try
// forever. Four restarts fit inside the default budget; a job that dies on
// every attempt reaches the end of it and is failed, for a reason that is
// its own.
func TestRunnerRecoverStopsInterruptLoops(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(2)

	j, err := s.Create("crashy", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Register("crashy", func(ctx context.Context, j Job) error { return nil })

	// Each cycle: claim, die, flag, re-queue — until the budget runs out.
	final := j
	for i := 0; i < 6; i++ {
		if _, err := s.Start(j.ID); err != nil {
			t.Fatalf("cycle %d start: %v", i, err)
		}
		if _, err := s.MarkInterrupted(); err != nil {
			t.Fatalf("cycle %d mark: %v", i, err)
		}
		if _, err := r.Recover(); err != nil {
			t.Fatalf("cycle %d recover: %v", i, err)
		}
		final, _ = s.Get(j.ID)
		if terminal(final.Status) {
			break
		}
	}

	if final.Status != StatusFailed {
		t.Fatalf("status = %s after repeated interruptions, want failed", final.Status)
	}
	if final.Failures < 2 {
		t.Fatalf("failures = %d, want at least the configured budget", final.Failures)
	}
	if !strings.Contains(final.Error, "interrupted") {
		t.Fatalf("error = %q, want to say the work kept getting interrupted", final.Error)
	}
}

// A handler that panics is a failed attempt, not a dead runner. The
// failure has to be recorded so the budget, rather than the crash, is what
// eventually stops the work.
func TestRunnerRecordsPanicAsFailedAttempt(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(1)

	r.Register("boom", func(ctx context.Context, j Job) error {
		panic("handler exploded")
	})

	j, err := s.Create("boom", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())

	got := waitJob(t, s, j.ID, StatusFailed, 10*time.Second)
	if !strings.Contains(got.Error, "panicked") {
		t.Fatalf("error = %q, want it to name the panic", got.Error)
	}
	waitIdle(t, r)
}

// Work nobody can execute must be left readable as pending, not run by
// whatever happens to be registered next door and not quietly failed.
func TestRunnerLeavesUnknownKindAlone(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.Register("known", func(ctx context.Context, j Job) error { return nil })

	j, err := s.Create("unknown", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())
	time.Sleep(80 * time.Millisecond)

	got, err := s.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPending || got.Attempts != 0 {
		t.Fatalf("job = %s attempts=%d, want pending and unattempted", got.Status, got.Attempts)
	}
}

// Work scheduled for later is not due yet: the backoff window is a wait,
// and a runner that ignored it would turn every delay into an immediate
// second attempt.
func TestRunnerHoldsWorkUntilItsAttemptTime(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)

	r.Register("slow", func(ctx context.Context, j Job) error { return nil })

	j, err := s.Create("slow", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleRetry(j.ID, 400*time.Millisecond, "provider asked us to come back"); err != nil {
		t.Fatal(err)
	}

	r.Start(context.Background())
	time.Sleep(150 * time.Millisecond)
	early, _ := s.Get(j.ID)
	if early.Attempts != 1 {
		t.Fatalf("attempts = %d before the window opened, want 1", early.Attempts)
	}

	got := waitJob(t, s, j.ID, StatusSucceeded, 10*time.Second)
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", got.Attempts)
	}
	if got.Failures != 1 {
		t.Fatalf("failures = %d, want 1", got.Failures)
	}
}

// Only jobs that are actually due come back from ListDue, oldest first —
// it is the single query the loop makes, so what it decides not to return
// is what the runner does not touch.
func TestListDueReturnsOnlyJobsWhoseTimeHasCome(t *testing.T) {
	s := newTestStore(t, nil)

	nowJob, err := s.Create("a", "sess", nil)
	if err != nil {
		t.Fatal(err)
	}
	later, err := s.Create("b", "sess", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(later.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleRetry(later.ID, time.Hour, "later"); err != nil {
		t.Fatal(err)
	}
	done, err := s.Create("c", "sess", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(done.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Succeed(done.ID); err != nil {
		t.Fatal(err)
	}

	due, err := s.ListDue(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != nowJob.ID {
		t.Fatalf("due = %+v, want only %s", due, nowJob.ID)
	}
}

// "Not now" is not a failure. A job that could not start because a slot
// was busy keeps its entire budget and simply waits — otherwise a busy
// minute could exhaust the attempts of work that never went wrong.
func TestRunnerSnoozesBusyWorkWithoutSpendingAttempts(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(1) // one real failure would be the whole budget

	var calls int32
	r.Register("busy", func(ctx context.Context, j Job) error {
		if atomic.AddInt32(&calls, 1) < 2 {
			return Busy(time.Second, "no slot free")
		}
		return nil
	})

	j, err := s.Create("busy", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(context.Background())

	got := waitJob(t, s, j.ID, StatusSucceeded, 15*time.Second)
	if got.Failures != 0 {
		t.Fatalf("failures = %d, want 0: waiting for a slot is not a failure", got.Failures)
	}
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", got.Attempts)
	}
}

// Recovery is not abandoned when it runs too early. This runtime starts
// the runner from inside the agent loop, before the schema migration runs,
// so the first Recover can fail on a jobs table that does not yet have the
// retry columns — and the loop must try again rather than strand exactly
// the work recovery exists to save.
func TestRunnerRetriesRecoveryUntilTheSchemaIsReady(t *testing.T) {
	db := openMem(t)
	if _, err := db.Exec(v1JobsDDL); err != nil {
		t.Fatal(err)
	}
	// Bring the table up to everything except the retry columns: the exact
	// state at the moment the runner first starts.
	if err := EnsureV2Columns(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTrajectoryColumn(db); err != nil {
		t.Fatal(err)
	}
	// A job a previous process left running.
	if _, err := db.Exec(`INSERT INTO jobs (id, kind, status, checkpoints, payload, attempts, created_at, updated_at) VALUES ('job-left','crawl','running','[]','{}',1,0,0)`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db, nil)
	r := newTestRunner(t, s)
	var ran int32
	r.Register("crawl", func(ctx context.Context, j Job) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})

	if _, err := s.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}

	// Too early: the retry columns do not exist, so recovery cannot succeed.
	if _, err := r.Recover(); err == nil {
		t.Fatal("expected recovery to fail before the retry columns exist")
	}

	// The migration runs, then the next tick tries recovery again.
	if err := EnsureRetryColumns(db); err != nil {
		t.Fatal(err)
	}
	r.Tick(context.Background())

	got := waitJob(t, s, "job-left", StatusSucceeded, 5*time.Second)
	r.Wait()
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (the crashed start plus the recovery run)", got.Attempts)
	}
	if atomic.LoadInt32(&ran) != 1 {
		t.Fatalf("handler ran %d times, want 1", ran)
	}
}

// A job that has had its ceiling raised gets the new one — the owner
// saying "try again, more" is a decision, and the store has to honour it
// rather than remember the old number.
func TestRunnerHonoursARaisedCeiling(t *testing.T) {
	s := newTestStore(t, nil)
	r := newTestRunner(t, s)
	r.SetDefaultMaxAttempts(1)

	var calls int32
	r.Register("flaky", func(ctx context.Context, j Job) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			return errors.New("first attempt failed")
		}
		return nil
	})

	j, err := s.Create("flaky", "sess1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Give it room before the runner sees it: one failure would otherwise
	// be the whole budget.
	if err := s.SetRetryPolicy(j.ID, 3); err != nil {
		t.Fatal(err)
	}

	r.Start(context.Background())
	got := waitJob(t, s, j.ID, StatusSucceeded, 10*time.Second)
	if got.Failures != 1 {
		t.Fatalf("failures = %d, want 1", got.Failures)
	}
	if got.MaxAttempts != 3 {
		t.Fatalf("max_attempts = %d, want the policy written on the job", got.MaxAttempts)
	}
}
