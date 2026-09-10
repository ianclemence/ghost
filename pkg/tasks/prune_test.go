package tasks

import (
	"testing"
	"time"
)

// PruneFinished removes terminal jobs past the cutoff and never touches a
// live or interrupted (restartable) job.
func TestPruneFinished(t *testing.T) {
	s := newTestStore(t, nil)
	done, err := s.Create("k", "s-done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(done.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Succeed(done.ID); err != nil {
		t.Fatal(err)
	}
	live, err := s.Create("k", "s-live", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(live.ID); err != nil {
		t.Fatal(err)
	}
	// An interrupted job is restartable and must survive.
	interrupted, err := s.Create("k", "s-int", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(interrupted.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneFinished(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	if _, err := s.Get(done.ID); err == nil {
		t.Fatal("finished job must be pruned")
	}
	if _, err := s.Get(live.ID); err != nil {
		t.Fatal("live job must survive")
	}
	if _, err := s.Get(interrupted.ID); err != nil {
		t.Fatal("interrupted job must survive (restartable)")
	}
}
