package turnlog

import (
	"testing"
	"time"
)

// PruneTerminal removes terminal turns past the cutoff and never touches a
// live turn. Terminal records exist only for the reconnect replay window.
func TestPruneTerminal(t *testing.T) {
	s := newStore(t)
	if _, err := s.Claim("c", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("c", "done", StatusCompleted, "success"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("c", "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("c", "failed", StatusFailed, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("c", "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("c", "live", StatusRunning, ""); err != nil {
		t.Fatal(err)
	}

	// A future cutoff prunes every terminal turn.
	n, err := s.PruneTerminal(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 pruned, got %d", n)
	}
	if got, _ := s.Get("c", "done"); got != nil {
		t.Fatal("terminal completed turn must be pruned")
	}
	if got, _ := s.Get("c", "failed"); got != nil {
		t.Fatal("terminal failed turn must be pruned")
	}
	if got, _ := s.Get("c", "live"); got == nil {
		t.Fatal("live turn must never be pruned")
	}
}
