package tasks

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T, events *[]string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	// Schema (duplicated minimally; real Ghost uses pkg/db).
	exec := `CREATE TABLE jobs (
		id TEXT PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL,
		progress REAL NOT NULL DEFAULT 0, checkpoints JSON, payload JSON,
		session_key TEXT, error TEXT, attempts INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL, started_at DATETIME, finished_at DATETIME,
		updated_at DATETIME NOT NULL)`
	if _, err := db.Exec(exec); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db, func(kind string, job Job) {
		if events != nil {
			*events = append(*events, kind)
		}
	})
	t.Cleanup(func() { db.Close() })
	return s
}

func TestTaskLifecycle(t *testing.T) {
	var events []string
	s := newTestStore(t, &events)

	j, err := s.Create("automation", "sess1", map[string]interface{}{"q": "weather"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if j.Status != StatusPending {
		t.Fatalf("expected pending, got %s", j.Status)
	}

	started, err := s.Start(j.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Status != StatusRunning {
		t.Fatalf("expected running, got %s", started.Status)
	}
	if started.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", started.Attempts)
	}
	if started.StartedAt == nil {
		t.Fatal("expected started_at set")
	}

	prog, err := s.Progress(j.ID, 0.5, "fetched")
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(prog.Checkpoints) != 1 || prog.Checkpoints[0] != "fetched" {
		t.Fatalf("expected checkpoint, got %+v", prog.Checkpoints)
	}
	if prog.Progress != 0.5 {
		t.Fatalf("expected progress 0.5, got %v", prog.Progress)
	}

	done, err := s.Succeed(j.ID)
	if err != nil {
		t.Fatalf("succeed: %v", err)
	}
	if done.Status != StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", done.Status)
	}
	if done.FinishedAt == nil {
		t.Fatal("expected finished_at set")
	}
	if !contains(events, EventDone) {
		t.Fatalf("expected completed event, got %v", events)
	}
}

func TestTaskFailRetryResume(t *testing.T) {
	var events []string
	s := newTestStore(t, &events)

	j, _ := s.Create("task", "s2", nil)
	s.Start(j.ID)
	if _, err := s.Fail(j.ID, "boom"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	failed, _ := s.Get(j.ID)
	if failed.Status != StatusFailed || failed.Error != "boom" {
		t.Fatalf("expected failed+error, got %+v", failed)
	}
	if !contains(events, EventFailed) {
		t.Fatalf("expected failed event, got %v", events)
	}

	// Retry returns it to pending for another attempt.
	r, rerr := s.Retry(j.ID)
	if rerr != nil {
		t.Fatalf("retry: %v", rerr)
	}
	if r.Status != StatusPending {
		t.Fatalf("expected pending after retry, got %s (err=%v)", r.Status, rerr)
	}
	if r.Error != "" {
		t.Fatalf("expected cleared error after retry, got %q", r.Error)
	}
	if !contains(events, EventRetrying) {
		t.Fatalf("expected retrying event, got %v", events)
	}
}

func TestTaskMarkInterruptedOnRestart(t *testing.T) {
	s := newTestStore(t, nil)
	j, _ := s.Create("task", "s3", nil)
	s.Start(j.ID)
	if _, err := s.MarkInterrupted(); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	got, _ := s.Get(j.ID)
	if got.Status != StatusInterrupted {
		t.Fatalf("expected interrupted after restart, got %s", got.Status)
	}
	// Interrupted jobs are resumable.
	if _, err := s.Retry(j.ID); err != nil {
		t.Fatalf("retry interrupted: %v", err)
	}
	r, _ := s.Get(j.ID)
	if r.Status != StatusPending {
		t.Fatalf("expected pending after retry, got %s", r.Status)
	}
}

func TestTaskListFilter(t *testing.T) {
	s := newTestStore(t, nil)
	a, _ := s.Create("a", "s", nil)
	b, _ := s.Create("b", "s", nil)
	s.Start(a.ID)
	s.Succeed(a.ID)

	all, err := s.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(all))
	}
	running, _ := s.List(StatusRunning)
	if len(running) != 0 {
		t.Fatalf("expected 0 running jobs (b was never started), got %+v", running)
	}
	pending, _ := s.List(StatusPending)
	if len(pending) != 1 || pending[0].ID != b.ID {
		t.Fatalf("expected 1 pending job (b), got %+v", pending)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
