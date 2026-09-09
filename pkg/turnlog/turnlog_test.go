package turnlog

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "turns"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Identical requests must resolve to ONE winner — the core "reconnect is not
// re-submission" invariant that prevents duplicate side effects.
func TestClaimIsOnce(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	results := make([]ClaimResult, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := s.Claim("conv-1", "req-1")
			if err == nil {
				results[idx] = res
			}
		}(i)
	}
	wg.Wait()
	winners := 0
	others := 0
	for _, r := range results {
		if r.Created {
			winners++
		} else {
			others++
			if r.Turn == nil || r.Turn.Status == "" {
				t.Fatalf("repeat claim must attach to the existing turn, got %+v", r)
			}
		}
	}
	if winners != 1 || others != 9 {
		t.Fatalf("exactly one winner expected, got winners=%d others=%d", winners, others)
	}
}

// A completed turn stays terminal: a repeat returns the outcome and can never
// execute again.
func TestTerminalOutcomePreserved(t *testing.T) {
	s := newStore(t)
	if res, err := s.Claim("c", "r"); err != nil || !res.Created {
		t.Fatalf("claim: %v %v", res, err)
	}
	if _, err := s.Set("c", "r", StatusCompleted, "success"); err != nil {
		t.Fatal(err)
	}
	again, err := s.Claim("c", "r")
	if err != nil {
		t.Fatal(err)
	}
	if again.Created {
		t.Fatal("completed turn must never be re-executed")
	}
	if again.Status != StatusCompleted || again.Turn.Outcome != "success" {
		t.Fatalf("terminal outcome must be preserved: %+v", again)
	}
}

// A crash mid-turn must not leave a permanent "running" lock that refuses
// every reconnect forever.
func TestRecoverInterruptsStaleTurns(t *testing.T) {
	s := newStore(t)
	s.Claim("c", "r")
	if _, err := s.Set("c", "r", StatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(time.Second) // a NEW process start AFTER the turn began
	n, err := s.Recover(start)
	if err != nil || n != 1 {
		t.Fatalf("recover: n=%d err=%v", n, err)
	}
	turn, _ := s.Get("c", "r")
	if turn.Status != StatusInterrupted {
		t.Fatalf("stale running turn must be interrupted, got %s", turn.Status)
	}
	// A NEW claim after recovery is allowed (the stale turn is no longer a
	// live execution), but only because the old one is truthfully marked
	// interrupted — never silently double-executing.
	res, err := s.Claim("c", "r")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusInterrupted {
		t.Fatalf("repeat claim after recovery must report interrupted, got %s", res.Status)
	}
}
