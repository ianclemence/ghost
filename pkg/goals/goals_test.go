package goals

import (
	"testing"
	"time"
)

func TestGoalLifecycle(t *testing.T) {
	s := NewStore(t.TempDir())
	g, err := s.Create("take care of school emails", "school.edu inbox", "inbox triaged daily", []string{"email.read"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusActive || g.ID == "" {
		t.Fatalf("bad create: %+v", g)
	}
	list, err := s.List(time.Now())
	if err != nil || len(list) != 1 || !list[0].Usable(time.Now()) {
		t.Fatalf("usable active goal expected: %+v %v", list, err)
	}
	if _, err := s.Pause(g.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(time.Now()); list[0].Usable(time.Now()) {
		t.Fatal("paused goal must not be usable")
	}
	if _, err := s.Resume(g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendProgress(g.ID, "triaged 12, 2 need owner"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(g.ID)
	if err != nil || len(got.Progress) != 1 {
		t.Fatalf("progress must persist: %+v %v", got, err)
	}
	if _, err := s.Complete(g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resume(g.ID); err == nil {
		t.Fatal("completed goal must not resume")
	}
}

func TestGoalExpiryAutoPauses(t *testing.T) {
	s := NewStore(t.TempDir())
	g, err := s.Create("temp", "", "", nil, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List(time.Now())
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	if list[0].Status != StatusExpired {
		t.Fatalf("expired goal must auto-pause, got %s", list[0].Status)
	}
	if list[0].Usable(time.Now()) {
		t.Fatal("expired goal must not be usable")
	}
	_ = g
}

func TestGoalNarrowingOnly(t *testing.T) {
	s := NewStore(t.TempDir())
	g, err := s.Create("scope test", "", "", []string{"email.read"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.NarrowCapabilities(g.ID, []string{"email.read", "email.send"}); err == nil {
		t.Fatal("widening capabilities must be refused")
	}
	narrowed, err := s.NarrowCapabilities(g.ID, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed.Capabilities) != 0 {
		t.Fatal("narrowing to empty must succeed")
	}
}

func TestGoalLinkRoutineIdempotent(t *testing.T) {
	s := NewStore(t.TempDir())
	g, err := s.Create("linked", "", "", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkRoutine(g.ID, "r1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.LinkRoutine(g.ID, "r1")
	if err != nil || len(got.RoutineIDs) != 1 {
		t.Fatalf("link must be idempotent: %+v %v", got, err)
	}
}
