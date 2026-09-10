package browser

import (
	"testing"
	"time"
)

// CloseForTask ends exactly the sessions bound to one task and leaves other
// tasks' sessions live. A terminal task must not leave a browser session (and
// its profile) behind.
func TestCloseForTask(t *testing.T) {
	s := openTestStore(t)
	a, err := s.GetOrCreate("owner", "personal", "task-a", "default", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.GetOrCreate("owner", "personal", "task-b", "default", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CloseForTask("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 closed, got %d", n)
	}
	// task-a's closed session is not reused; a fresh one is minted.
	a2, err := s.GetOrCreate("owner", "personal", "task-a", "default", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if a2.ID == a.ID {
		t.Fatal("closed task session must not be reused")
	}
	// An unrelated task keeps its session.
	b2, err := s.GetOrCreate("owner", "personal", "task-b", "default", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if b2.ID != b.ID {
		t.Fatal("unrelated task session must survive")
	}

	// Empty task id is a no-op.
	if n, err := s.CloseForTask(""); err != nil || n != 0 {
		t.Fatalf("empty task id must be a no-op, got %d %v", n, err)
	}
}
