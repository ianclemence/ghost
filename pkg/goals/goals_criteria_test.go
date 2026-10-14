package goals

import (
	"strings"
	"testing"
	"time"
)

func newGoal(t *testing.T) (*Store, Goal) {
	t.Helper()
	s := NewStore(t.TempDir())
	g, err := s.Create("ship the site", "", "", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return s, g
}

func TestGoalWithoutCriteriaCompletesAsBefore(t *testing.T) {
	s, g := newGoal(t)
	if _, err := s.Complete(g.ID); err != nil {
		t.Fatalf("a goal with no criteria must still complete: %v", err)
	}
}

func TestGoalCannotCompleteUntilEveryCriterionPassesWithEvidence(t *testing.T) {
	s, g := newGoal(t)
	if _, err := s.SetCriteria(g.ID, []string{"home page loads", "contact form works"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(g.ID); err == nil || !strings.Contains(err.Error(), "contact form works") {
		t.Fatalf("completion must name what is unmet: %v", err)
	}
	if _, err := s.CheckCriterion(g.ID, 1, "  "); err == nil {
		t.Fatal("a criterion cannot pass without evidence")
	}
	if _, err := s.CheckCriterion(g.ID, 1, "opened it at phone size, hero and menu render"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(g.ID); err == nil {
		t.Fatal("one unmet criterion must still block completion")
	}
	if _, err := s.CheckCriterion(g.ID, 2, "submitted the form, saw the confirmation"); err != nil {
		t.Fatal(err)
	}
	done, err := s.Complete(g.ID)
	if err != nil || done.Status != StatusCompleted {
		t.Fatalf("every criterion passed: %+v %v", done, err)
	}
}

func TestCriteriaAreBoundedAndNumbered(t *testing.T) {
	s, g := newGoal(t)
	if _, err := s.SetCriteria(g.ID, nil); err == nil {
		t.Fatal("an empty list is not criteria")
	}
	if _, err := s.SetCriteria(g.ID, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckCriterion(g.ID, 5, "evidence"); err == nil {
		t.Fatal("a number outside the list must be refused")
	}
}
