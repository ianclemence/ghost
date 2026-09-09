package deviceops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "device-ops"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLifecycleDurable(t *testing.T) {
	s := newStore(t)
	op, err := s.Create("restart", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if op.State != StateScheduled {
		t.Fatalf("state %s", op.State)
	}
	if _, err := s.Transition(op.ID, StateRebooting, ""); err != nil {
		t.Fatal(err)
	}
	// Reload from a fresh store over the same dir (process restart).
	s2, err := New(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(op.ID)
	if err != nil || got == nil || got.State != StateRebooting {
		t.Fatalf("durability failed: %+v err=%v", got, err)
	}
}

func TestReconcileAfterBoot(t *testing.T) {
	s := newStore(t)
	// A request created AFTER the process start must never be touched.
	op, _ := s.Create("restart", "device-a")
	start := time.Now().Add(-time.Hour) // pretend process booted an hour ago
	n, err := s.Reconcile(start, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("new op must not be auto-completed: n=%d err=%v", n, err)
	}
	if got, _ := s.Get(op.ID); got.State != StateScheduled {
		t.Fatalf("new op state must stay scheduled, got %s", got.State)
	}

	// A pre-restart operation (created before boot) is reconciled as
	// completed once the device returns.
	pre := &Operation{
		ID: "devop-999", Action: "update", State: StateRebooting,
		CreatedAt: time.Now().Add(-2 * time.Minute), UpdatedAt: time.Now().Add(-2 * time.Minute),
		RequestedBy: "device-a",
	}
	raw, _ := json.Marshal(pre)
	if err := os.WriteFile(filepath.Join(s.dir, pre.ID+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	n, err = s.Reconcile(now.Add(-time.Minute), now)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 reconciled, got %d err=%v", n, err)
	}
	got, _ := s.Get(pre.ID)
	if got.State != StateCompleted {
		t.Fatalf("pre-restart op must complete after return, got %s", got.State)
	}
}

func TestForgedAndUnknownFail(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create("rm -rf /", "x"); err == nil {
		t.Fatal("unknown action must fail")
	}
	if _, err := s.Get("../../etc/passwd"); err == nil {
		t.Fatal("forged id must fail")
	}
	if _, err := s.Transition("does-not-exist", StateCompleted, ""); err == nil {
		t.Fatal("unknown op must fail")
	}
}

func TestTerminalImmutable(t *testing.T) {
	s := newStore(t)
	op, _ := s.Create("restart", "a")
	if _, err := s.Transition(op.ID, StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(op.ID, StateFailed, "later"); err == nil {
		t.Fatal("terminal op must be immutable")
	}
}
