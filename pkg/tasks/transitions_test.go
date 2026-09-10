package tasks

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	_ "modernc.org/sqlite"
)

func openStream(t *testing.T, db *sql.DB) *cevents.Stream {
	t.Helper()
	st, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Moves that would make dead work live again fail loudly with the current
// state attached — the caller must not proceed as if the job restarted.
func TestTerminalTransitionsRejected(t *testing.T) {
	s := newTestStore(t, nil)
	j, _ := s.Create("k", "sess", nil)
	s.Start(j.ID)
	if _, err := s.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}

	var terr *TransitionError
	for op, call := range map[string]func() (Job, error){
		"start":    func() (Job, error) { return s.Start(j.ID) },
		"resume":   func() (Job, error) { return s.Resume(j.ID) },
		"retry":    func() (Job, error) { return s.Retry(j.ID) },
		"pause":    func() (Job, error) { return s.Pause(j.ID) },
		"expire":   func() (Job, error) { return s.Expire(j.ID) },
		"wait":     func() (Job, error) { return s.SetWaiting(j.ID, StatusWaitingUser, "x") },
		"finish":   func() (Job, error) { return s.Fail(j.ID, "late") },
		"progress": func() (Job, error) { return s.Progress(j.ID, 0.9, "late") },
	} {
		got, err := call()
		if err == nil {
			t.Fatalf("%s on a succeeded job must be rejected", op)
		}
		if !errors.As(err, &terr) {
			t.Fatalf("%s: want *TransitionError, got %T (%v)", op, err, err)
		}
		if got.Status != StatusSucceeded {
			t.Fatalf("%s must leave state untouched, got %s", op, got.Status)
		}
	}
	again, _ := s.Get(j.ID)
	if again.Status != StatusSucceeded {
		t.Fatalf("rejected moves must not mutate, got %s", again.Status)
	}
}

// Repeating the SAME terminal outcome stays silent (idempotent completion;
// first outcome wins, duplicates acknowledge it).
func TestSameOutcomeIdempotent(t *testing.T) {
	s := newTestStore(t, nil)
	j, _ := s.Create("k", "sess", nil)
	s.Start(j.ID)
	if _, err := s.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Succeed(j.ID); err != nil {
		t.Fatalf("repeat succeed must stay silent: %v", err)
	}
	j2, _ := s.Create("k", "sess", nil)
	s.Start(j2.ID)
	if _, err := s.Cancel(j2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(j2.ID); err != nil {
		t.Fatalf("repeat cancel must stay silent: %v", err)
	}
}

// Crash recovery rotates generations so a zombie worker's late completion
// (old generation) can never land on the resumed execution.
func TestMarkInterruptedRotatesGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec.db")
	s := openStoreAt(t, path, nil)
	j, _ := s.Create("k", "sess", nil)
	s.Start(j.ID)
	oldGen := j.Generation
	if _, err := s.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(j.ID)
	if got.Status != StatusInterrupted {
		t.Fatalf("status = %s", got.Status)
	}
	if got.Generation == "" || got.Generation == oldGen {
		t.Fatal("recovery must rotate the generation")
	}
	if s.CheckGeneration(j.ID, oldGen) {
		t.Fatal("old generation must not verify after recovery")
	}
}

// Lifecycle transitions mirror onto the canonical stream as durable
// evidence carrying the job identity (no trajectory: background origin).
func TestTaskEventsPublishedToStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ev.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	st := openStream(t, db)
	s := NewStore(db, nil)
	s.SetEventStream(st)

	j, _ := s.Create("browse", "sess-1", nil)
	s.Start(j.ID)
	s.Progress(j.ID, 0.5, "step one done")
	s.SetWaiting(j.ID, StatusWaitingUser, "need input")
	s.Resume(j.ID)
	s.Start(j.ID)
	s.Fail(j.ID, "boom")

	rows, err := db.Query(`SELECT type, status FROM canonical_events ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	statusByKind := map[string]string{}
	for rows.Next() {
		var typ, status string
		if err := rows.Scan(&typ, &status); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, typ)
		statusByKind[typ] = status
	}
	for _, want := range []string{
		"task.created", "task.started", "task.checkpointed",
		"task.waiting", "task.resumed", "task.failed",
	} {
		found := false
		for _, k := range kinds {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing durable %s (have %v)", want, kinds)
		}
	}
	// Bare progress heartbeats (no checkpoint) stay out of the warehouse.
	for _, k := range kinds {
		if k == "task.progress" {
			t.Fatal("bare progress must not publish")
		}
	}
	// Payload carries the job identity for forensics.
	var payload string
	if err := db.QueryRow(`SELECT payload FROM canonical_events WHERE type='task.failed'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload == "" || !containsStr(payload, j.ID) {
		t.Fatalf("failed payload must name the job: %s", payload)
	}
}

// A nil stream disables durable emission (unwired stores behave as before).
func TestTaskEventsNilStreamSafe(t *testing.T) {
	s := newTestStore(t, nil)
	s.SetEventStream(nil)
	j, _ := s.Create("k", "sess", nil)
	s.Start(j.ID)
	if _, err := s.Succeed(j.ID); err != nil {
		t.Fatal(err)
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
