package turnlog

import (
	"path/filepath"
	"testing"
	"time"
)

// driveTurn claims a turn, marks it running and records where it came from,
// which is exactly the state a live reply is in while it streams.
func driveTurn(t *testing.T, s *Store, session, request, text, channel string) *Turn {
	t.Helper()
	if res, err := s.Claim(session, request); err != nil || !res.Created {
		t.Fatalf("claim: %v %v", res, err)
	}
	if _, err := s.Set(session, request, StatusRunning, ""); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := s.SetOrigin(session, request, text, channel, "default"); err != nil {
		t.Fatalf("set origin: %v", err)
	}
	turn, err := s.Get(session, request)
	if err != nil || turn == nil {
		t.Fatalf("get: %v %v", turn, err)
	}
	return turn
}

// The reply a model streams used to live only in memory, so a restart
// mid-sentence threw away every word already spoken. It must be on disk
// while the turn is still running.
func TestCheckpointPersistsPartialReply(t *testing.T) {
	s := newStore(t)
	driveTurn(t, s, "main", "req-1", "what time is it", "mobile")

	if err := s.Checkpoint("main", "req-1", "It's half past ", false); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	got, err := s.Get("main", "req-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Partial != "It's half past " {
		t.Fatalf("partial not persisted, got %q", got.Partial)
	}
	if got.PartialAt.IsZero() {
		t.Fatal("PartialAt must record when the reply was last flushed")
	}
}

// A token stream must not become a stream of disk writes: consecutive
// checkpoints inside the throttle window are dropped unless the reply grew
// by a real step. force bypasses it for the final flush.
func TestCheckpointIsThrottled(t *testing.T) {
	s := newStore(t)
	driveTurn(t, s, "main", "req-1", "q", "mobile")

	if err := s.Checkpoint("main", "req-1", "aaaa", false); err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	// Same instant, one byte later: under both thresholds, so dropped.
	if err := s.Checkpoint("main", "req-1", "aaaab", false); err != nil {
		t.Fatalf("throttled checkpoint: %v", err)
	}
	got, _ := s.Get("main", "req-1")
	if got.Partial != "aaaa" {
		t.Fatalf("throttle did not hold, partial=%q", got.Partial)
	}
	// Growth past the byte step wins over the timer.
	big := make([]byte, checkpointByteStep+8)
	for i := range big {
		big[i] = 'x'
	}
	if err := s.Checkpoint("main", "req-1", string(big), false); err != nil {
		t.Fatalf("growth checkpoint: %v", err)
	}
	got, _ = s.Get("main", "req-1")
	if len(got.Partial) != len(big) {
		t.Fatalf("growth past the step must flush, got %d bytes", len(got.Partial))
	}
	// force flushes regardless of either threshold.
	if err := s.Checkpoint("main", "req-1", "z", true); err != nil {
		t.Fatalf("forced checkpoint: %v", err)
	}
	got, _ = s.Get("main", "req-1")
	if got.Partial != "z" {
		t.Fatalf("forced checkpoint ignored, partial=%q", got.Partial)
	}
}

// A finished reply is in the transcript, so its checkpoint must be retired —
// otherwise recovery would append the same words a second time.
func TestCompletedTurnRetiresItsPartial(t *testing.T) {
	s := newStore(t)
	driveTurn(t, s, "main", "req-1", "q", "mobile")
	_ = s.Checkpoint("main", "req-1", "half a reply", true)

	if _, err := s.Set("main", "req-1", StatusCompleted, "success"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, _ := s.Get("main", "req-1")
	if got.Partial != "" {
		t.Fatalf("completed turn kept a partial (%q), which would duplicate history", got.Partial)
	}
	if !got.Materialized {
		t.Fatal("completed turn must be marked as already in the transcript")
	}
	// And it cannot be checkpointed back into existence.
	if err := s.Checkpoint("main", "req-1", "zombie", true); err != nil {
		t.Fatalf("checkpoint after terminal: %v", err)
	}
	got, _ = s.Get("main", "req-1")
	if got.Partial != "" {
		t.Fatalf("terminal turn accepted a checkpoint: %q", got.Partial)
	}
}

// The whole point: a process that dies mid-reply leaves the words on disk,
// recovery marks the turn interrupted, and the half-reply comes back ready
// to be written into the transcript exactly once.
func TestCrashLeavesTheReplyRecoverable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "turns")
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	driveTurn(t, s, "main", "req-1", "summarise my week", "mobile")
	_ = s.Checkpoint("main", "req-1", "You shipped three things and ", true)

	// The process dies. A new one opens the same directory.
	s2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery's process start must postdate the turn's creation, as it
	// always does after a real restart.
	if _, err := s2.Recover(time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("recover: %v", err)
	}

	got, _ := s2.Get("main", "req-1")
	if got.Status != StatusInterrupted {
		t.Fatalf("crashed turn must read interrupted, got %q", got.Status)
	}
	if got.Partial != "You shipped three things and " {
		t.Fatalf("the reply did not survive the crash, got %q", got.Partial)
	}
	if got.InterruptedAt.IsZero() {
		t.Fatal("InterruptedAt must record when the turn was last alive")
	}

	pending, err := s2.PendingPartials()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Partial != "You shipped three things and " {
		t.Fatalf("expected one recoverable partial, got %+v", pending)
	}

	// Writing it into the transcript happens once, and can never repeat.
	if err := s2.MarkMaterialized("main", "req-1"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if pending, _ := s2.PendingPartials(); len(pending) != 0 {
		t.Fatalf("partial materialized twice: %+v", pending)
	}
	got, _ = s2.Get("main", "req-1")
	if got.ResumedBy != "" {
		t.Fatalf("unexpected resume link %q", got.ResumedBy)
	}
}

// A turn that produced no reply yet is still worth resuming (the owner did
// ask), but it has nothing to write into the transcript.
func TestCrashBeforeFirstTokenLeavesNoPartial(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "turns")
	s, _ := New(dir)
	driveTurn(t, s, "main", "req-1", "q", "mobile")

	s2, _ := New(dir)
	if _, err := s2.Recover(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := s2.Get("main", "req-1")
	if got.Status != StatusInterrupted {
		t.Fatalf("expected interrupted, got %q", got.Status)
	}
	if pending, _ := s2.PendingPartials(); len(pending) != 0 {
		t.Fatalf("nothing was written, so nothing may be recovered: %+v", pending)
	}
	if res, err := s2.Resumable(time.Now(), time.Minute); err != nil || len(res) != 1 {
		t.Fatalf("a turn with no tokens still owes the owner an answer: %v %v", res, err)
	}
}

// Freshness is measured from the last moment the turn was alive, so a Pod
// that was off all night does not resurrect a turn nobody is waiting on.
func TestResumableHonoursTheWindow(t *testing.T) {
	s := newStore(t)
	driveTurn(t, s, "main", "req-1", "q", "mobile")
	_ = s.Checkpoint("main", "req-1", "half", true)
	if _, err := s.Recover(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s2 := s
	got, _ := s2.Get("main", "req-1")
	dead := got.InterruptedAt
	if dead.IsZero() {
		t.Fatal("expected an interruption time")
	}

	// Still fresh.
	if res, err := s2.Resumable(dead.Add(time.Minute), time.Minute); err != nil || len(res) != 1 {
		t.Fatalf("fresh turn should resume: %v %v", res, err)
	}
	// Two hours later, nobody is waiting for it.
	if res, err := s2.Resumable(dead.Add(2*time.Hour), time.Minute); err != nil || len(res) != 0 {
		t.Fatalf("stale turn must be left alone: %v %v", res, err)
	}
	// Already picked back up: never resumed twice.
	if err := s2.MarkResumed("main", "req-1", "req-2"); err != nil {
		t.Fatal(err)
	}
	if res, err := s2.Resumable(dead.Add(time.Minute), time.Minute); err != nil || len(res) != 0 {
		t.Fatalf("a resumed turn must not be resumed again: %v %v", res, err)
	}
}

// Finishing an old reply once the owner has already moved on would drop a
// stale answer into the middle of a newer conversation.
func TestHasNewerTurnGuardsOutOfOrderReplies(t *testing.T) {
	s := newStore(t)
	old := driveTurn(t, s, "main", "req-old", "first question", "mobile")

	if newer, _ := s.HasNewerTurn(old); newer {
		t.Fatal("the only turn cannot be newer than itself")
	}

	later := driveTurn(t, s, "main", "req-new", "second question", "mobile")
	if newer, _ := s.HasNewerTurn(old); !newer {
		t.Fatal("a later turn in the same conversation must block the resume")
	}
	// A different conversation is not this turn's future.
	driveTurn(t, s, "other", "req-x", "unrelated", "mobile")
	if newer, _ := s.HasNewerTurn(later); newer {
		t.Fatal("the newest turn has nothing after it")
	}
}

// The origin is recorded once so a resumed turn can answer on the surface
// the owner actually spoke from.
func TestSetOriginIsRecorded(t *testing.T) {
	s := newStore(t)
	driveTurn(t, s, "main", "req-1", "what's the weather", "telegram")
	got, _ := s.Get("main", "req-1")
	if got.UserText != "what's the weather" || got.Channel != "telegram" || got.ChatID != "default" {
		t.Fatalf("origin not recorded: %+v", got)
	}
}
