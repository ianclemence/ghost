package tasks

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// openStoreAt opens a Store over a specific database file. Two stores over
// one file simulate a process restart: the "old process" never closes
// cleanly (no final flush beyond SQLite's own durability) and the "new
// process" reopens the same file.
func openStoreAt(t *testing.T, path string, events *[]string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	return NewStore(db, func(kind string, job Job) {
		if events != nil {
			*events = append(*events, kind)
		}
	})
}

// Scenario 1 (partial → crash → restart → resume → continue): a job runs,
// checkpoints evidence and resume state, the process dies, a fresh store
// reopens the file and finds it running, flags it interrupted, and the
// work resumes — then a STALE completion from the dead process is rejected
// by the rotated generation.
func TestCrashResumeRejectsStaleCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.db")
	s1 := openStoreAt(t, path, nil)

	j, err := s1.CreateWithScope("browse", "sess-1", "ian", "work", map[string]interface{}{"url": "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	oldGen := j.Generation
	if _, err := s1.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	// Partial work: step one done, durable cursor written.
	if err := s1.SetEvidence(j.ID, "step1: navigated"); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetResumeState(j.ID, "cursor:step2"); err != nil {
		t.Fatal(err)
	}
	// Process dies mid-step-two. s1 goes out of scope WITHOUT finishing.

	// Restart: a fresh store over the same file.
	s2 := openStoreAt(t, path, nil)
	if _, err := s2.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusInterrupted || got.Evidence != "step1: navigated" || got.ResumeState != "cursor:step2" {
		t.Fatalf("restart must find durable progress: %+v", got)
	}

	// The new worker resumes: retry → rotate generation → run.
	if _, err := s2.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	newGen, err := s2.RotateGeneration(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newGen == oldGen {
		t.Fatal("resume must rotate the generation")
	}
	if _, err := s2.Start(j.ID); err != nil {
		t.Fatal(err)
	}

	// The dead process's delayed completion arrives and must be dropped.
	if s2.CheckGeneration(j.ID, oldGen) {
		t.Fatal("old generation must not verify after resume")
	}
	if !s2.CheckGeneration(j.ID, newGen) {
		t.Fatal("current generation must verify")
	}

	// The resumed worker finishes with its own generation.
	if _, err := s2.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}
	final, _ := s2.Get(j.ID)
	if final.Status != StatusSucceeded {
		t.Fatalf("final status = %s", final.Status)
	}
}

// Scenario 2 (waiting_for_permission → crash → restart → approve → resume):
// the waiting state and its reason survive the restart untouched.
func TestCrashWhileWaitingPermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wait.db")
	s1 := openStoreAt(t, path, nil)
	j, _ := s1.Create("browse", "sess-1", nil)
	s1.Start(j.ID)
	if _, err := s1.SetWaiting(j.ID, StatusWaitingPermission, "need approval to submit form"); err != nil {
		t.Fatal(err)
	}
	// crash.

	s2 := openStoreAt(t, path, nil)
	// MarkInterrupted must NOT touch waiting jobs: they are alive and
	// waiting, not orphaned by the crash.
	if _, err := s2.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}
	got, _ := s2.Get(j.ID)
	if got.Status != StatusWaitingPermission {
		t.Fatalf("waiting_for_permission must survive restart, got %s", got.Status)
	}
	if got.Evidence != "need approval to submit form" {
		t.Fatalf("waiting reason lost: %q", got.Evidence)
	}
	// Approve (external) then resume: waiting work returns to pending and
	// runs again. Waiting is live-blocked, not terminal.
	r, err := s2.Resume(j.ID)
	if err != nil {
		t.Fatalf("resume after approval: %v", err)
	}
	if r.Status != StatusPending {
		t.Fatalf("resumed status = %s", r.Status)
	}
	if _, err := s2.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}
}

// A cancelled job can never be flipped to succeeded by a stale worker.
func TestTerminalStatusNotOverwritten(t *testing.T) {
	s := newTestStore(t, nil)
	j, _ := s.Create("browse", "sess-1", nil)
	s.Start(j.ID)
	if _, err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	// A stale worker that never saw the cancellation reports success.
	done, _ := s.Succeed(j.ID)
	if done.Status != StatusCancelled {
		t.Fatalf("terminal cancelled must win, got %s", done.Status)
	}
	// Same for expire then fail.
	j2, _ := s.Create("browse", "sess-2", nil)
	s.Start(j2.ID)
	s.Expire(j2.ID)
	failed, _ := s.Fail(j2.ID, "late error")
	if failed.Status != StatusExpired {
		t.Fatalf("terminal expired must win, got %s", failed.Status)
	}
}

// Scenario 3 (waiting_for_user → crash → restart → user responds → resume):
// same persistence contract for user waits, and a second resume attempt
// (duplicate user response) does not re-run an already-advanced job.
func TestCrashWhileWaitingUserDuplicateResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.db")
	s1 := openStoreAt(t, path, nil)
	j, _ := s1.Create("browse", "sess-1", nil)
	s1.Start(j.ID)
	if _, err := s1.SetWaiting(j.ID, StatusWaitingUser, "need the OTP from your phone"); err != nil {
		t.Fatal(err)
	}
	// crash.

	s2 := openStoreAt(t, path, nil)
	s2.MarkInterrupted()
	got, _ := s2.Get(j.ID)
	if got.Status != StatusWaitingUser {
		t.Fatalf("waiting_for_user must survive restart, got %s", got.Status)
	}
	// User responds; job resumes and runs to completion.
	if _, err := s2.RotateGeneration(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}

	// Duplicate user response (or a retried resume) arrives late: the job
	// is already terminal, so Resume must not resurrect it.
	late, _ := s2.Resume(j.ID)
	if late.Status != StatusSucceeded {
		t.Fatalf("late resume must not resurrect a finished job, got %s", late.Status)
	}
	// Retry of a terminal job is a no-op: it must not resurrect it.
	if _, err := s2.Retry(j.ID); err != nil {
		t.Fatalf("retry finished job: %v", err)
	}
	again, _ := s2.Get(j.ID)
	if again.Status != StatusSucceeded {
		t.Fatalf("retry must not resurrect a finished job, got %s", again.Status)
	}
}
