package personalcontext

import (
	"errors"
	"testing"
)

// Correction lifecycle: a belief is learned, corrected to a new value,
// contradicted, and resolved — reads always show exactly one current
// value, and every retirement keeps provenance.
func TestCorrectionLifecycle(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	// Learn: favorite color is blue.
	if _, err := s.Create(mkEntry("pc-blue", "user", "favorite_color", "blue")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Correct: actually it is green. Old value retires with a pointer.
	green, err := s.Supersede("user", "favorite_color", mkEntry("pc-green", "user", "favorite_color", "green"))
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	cur := s.Current()
	if len(cur) != 1 || cur[0].ID != green.ID {
		t.Fatalf("current = %+v, want only green", cur)
	}
	old, ok := s.Get("pc-blue")
	if !ok || old.Status != StatusSuperseded || old.SupersededBy == nil || *old.SupersededBy != green.ID {
		t.Fatalf("retired entry wrong: %+v %v", old, ok)
	}

	// Contradict: a second source says red. Model both as current, then
	// declare the conflict.
	if _, err := s.Create(mkEntry("pc-red", "user", "favorite_color", "red")); err != nil {
		t.Fatalf("create rival: %v", err)
	}
	if err := s.DeclareConflict("user", "favorite_color", green.ID, "pc-red"); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if cur := s.Current(); len(cur) != 0 {
		t.Fatalf("conflicted belief must vanish from current, got %+v", cur)
	}

	// Before the fix, this was a dead end: Supersede requires a current
	// entry and none exists while conflicted.
	if _, err := s.Supersede("user", "favorite_color", mkEntry("pc-x", "user", "favorite_color", "x")); err == nil {
		t.Fatal("supersede while conflicted must fail (no current entry)")
	}

	// Resolve: green wins. Red retires pointing at green; green is current.
	winner, err := s.ResolveConflict("user", "favorite_color", green.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if winner.Status != StatusCurrent {
		t.Fatalf("winner status = %q", winner.Status)
	}
	cur = s.Current()
	if len(cur) != 1 || cur[0].ID != green.ID {
		t.Fatalf("current after resolve = %+v", cur)
	}
	loser, _ := s.Get("pc-red")
	if loser.Status != StatusSuperseded || loser.SupersededBy == nil || *loser.SupersededBy != green.ID {
		t.Fatalf("loser wrong: %+v", loser)
	}

	// Correct again after resolution: the two-step correction works.
	yellow, err := s.Supersede("user", "favorite_color", mkEntry("pc-yellow", "user", "favorite_color", "yellow"))
	if err != nil {
		t.Fatalf("supersede after resolve: %v", err)
	}
	if cur := s.Current(); len(cur) != 1 || cur[0].ID != yellow.ID {
		t.Fatalf("current = %+v, want only yellow", cur)
	}
}

func TestResolveConflictRejects(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	if _, err := s.Create(mkEntry("a", "user", "food", "pizza")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkEntry("b", "user", "food", "pasta")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkEntry("c", "user", "drink", "tea")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeclareConflict("user", "food", "a", "b"); err != nil {
		t.Fatal(err)
	}

	// Unknown id.
	if _, err := s.ResolveConflict("user", "food", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown winner err = %v", err)
	}
	// Entry from another predicate.
	if _, err := s.ResolveConflict("user", "food", "c"); err == nil {
		t.Fatal("foreign entry must not win a food conflict")
	}
	// Non-conflicting (retired) entry: first resolve properly, then try
	// to crown the loser.
	if _, err := s.ResolveConflict("user", "food", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveConflict("user", "food", "b"); !errors.Is(err, ErrNotConflicting) {
		t.Fatalf("retired loser err = %v", err)
	}
	// Already-current winner: nothing to resolve.
	if _, err := s.ResolveConflict("user", "food", "a"); !errors.Is(err, ErrNotConflicting) {
		t.Fatalf("current entry err = %v", err)
	}
}
