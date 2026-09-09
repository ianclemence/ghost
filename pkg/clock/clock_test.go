package clock

import (
	"errors"
	"testing"
	"time"
)

func TestAssessInvalid(t *testing.T) {
	if got := assessAt(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)); got != Invalid {
		t.Fatalf("1970 must be invalid, got %v", got)
	}
	if got := assessAt(time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)); got != Invalid {
		t.Fatalf("2020 must be invalid, got %v", got)
	}
}

func TestAssessSyncStates(t *testing.T) {
	old := ntpSynchronized
	defer func() { ntpSynchronized = old }()
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)

	ntpSynchronized = func() (bool, error) { return true, nil }
	if got := assessAt(now); got != Synced {
		t.Fatalf("NTP-synced must be synced, got %v", got)
	}
	ntpSynchronized = func() (bool, error) { return false, nil }
	if got := assessAt(now); got != Unsynced {
		t.Fatalf("NTP-unsynced must be unsynced, got %v", got)
	}
	ntpSynchronized = func() (bool, error) { return false, errors.New("no timedatectl") }
	if got := assessAt(now); got != Unknown {
		t.Fatalf("missing timedatectl must be unknown, got %v", got)
	}
}

func TestGateBlocksOnlyInvalid(t *testing.T) {
	old := ntpSynchronized
	defer func() { ntpSynchronized = old }()

	// Drive the gate through assessAt-equivalent states by stubbing.
	ntpSynchronized = func() (bool, error) { return true, nil }
	if !NewGate(time.Millisecond).Safe() {
		t.Fatal("synced clock must allow scheduling")
	}
	ntpSynchronized = func() (bool, error) { return false, nil }
	if !NewGate(time.Millisecond).Safe() {
		t.Fatal("unsynced-but-sane clock must allow scheduling (offline-first)")
	}
	ntpSynchronized = func() (bool, error) { return false, errors.New("no timedatectl") }
	if !NewGate(time.Millisecond).Safe() {
		t.Fatal("unknown clock state must allow scheduling")
	}
}

func TestGateCaches(t *testing.T) {
	old := ntpSynchronized
	defer func() { ntpSynchronized = old }()
	calls := 0
	ntpSynchronized = func() (bool, error) { calls++; return true, nil }
	g := NewGate(time.Hour)
	g.Safe()
	g.Safe()
	if calls != 1 {
		t.Fatalf("gate must cache assessments, NTP queried %d times", calls)
	}
}

func TestWatcherDetectsForwardJump(t *testing.T) {
	var w Watcher
	base := time.Now()
	if w.Observe(base, base) {
		t.Fatal("first observation never reports a jump")
	}
	// Wall jumps 10 minutes with (almost) no monotonic passage.
	if !w.Observe(base.Add(10*time.Minute), time.Now()) {
		t.Fatal("10-minute forward jump must be detected")
	}
}

func TestWatcherIgnoresNormalTicking(t *testing.T) {
	var w Watcher
	base := time.Now()
	w.Observe(base, base)
	time.Sleep(20 * time.Millisecond)
	if w.Observe(time.Now(), time.Now()) {
		t.Fatal("normal ticking must not report a jump")
	}
}

func TestWatcherDetectsBackwardJump(t *testing.T) {
	var w Watcher
	base := time.Now()
	w.Observe(base, base)
	// Wall moves backward with monotonic passage: also a step.
	if !w.Observe(base.Add(-10*time.Minute), time.Now()) {
		t.Fatal("10-minute backward jump must be detected")
	}
}
