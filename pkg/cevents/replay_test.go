package cevents

import (
	"database/sql"
	"errors"
	"testing"
)

var errSimulated = errors.New("simulated handler failure")

func openReplayStream(t *testing.T) *Stream {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func publishDurable(t *testing.T, s *Stream, typ Type, req string) *Event {
	t.Helper()
	return s.Publish(&Event{Type: typ, RequestID: req})
}

func TestAckOnlyMovesForward(t *testing.T) {
	s := openReplayStream(t)
	if got := s.Checkpoint("c1"); got != 0 {
		t.Fatalf("unknown consumer must start at 0, got %d", got)
	}
	if err := s.Ack("c1", 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Ack("c1", 3); err != nil {
		t.Fatal(err)
	}
	if got := s.Checkpoint("c1"); got != 5 {
		t.Fatalf("checkpoint must stay 5, got %d", got)
	}
}

func TestClaimExactlyOnce(t *testing.T) {
	s := openReplayStream(t)
	first, err := s.Claim("c1", "evt-1")
	if err != nil || !first {
		t.Fatalf("first claim must win: %v %v", first, err)
	}
	second, err := s.Claim("c1", "evt-1")
	if err != nil || second {
		t.Fatalf("replay claim must lose: %v %v", second, err)
	}
	other, err := s.Claim("c2", "evt-1")
	if err != nil || !other {
		t.Fatal("a different consumer has its own claim space")
	}
}

// TestReplayAfterRestart simulates a crash: two events processed and
// acked, one published while down. The restarted consumer replays exactly
// the missed one, and a second replay is empty.
func TestReplayAfterRestart(t *testing.T) {
	s := openReplayStream(t)
	e1 := publishDurable(t, s, MemoryCreated, "r1")
	e2 := publishDurable(t, s, MemoryCreated, "r1")
	if err := s.Ack("c1", e2.Seq); err != nil {
		t.Fatal(err)
	}
	e3 := publishDurable(t, s, MemoryCreated, "r1")

	// Sanity: first two events really precede the third.
	if !(e1.Seq > 0 && e2.Seq > e1.Seq && e3.Seq > e2.Seq) {
		t.Fatalf("seq must be monotonic: %d %d %d", e1.Seq, e2.Seq, e3.Seq)
	}

	missed := s.Replay("c1", 100, Filter{})
	if len(missed) != 1 || missed[0].ID != e3.ID {
		t.Fatalf("must replay exactly the missed event, got %d", len(missed))
	}
	// Simulate processing + ack, then prove convergence: nothing left.
	first, err := s.Claim("c1", e3.ID)
	if err != nil || !first {
		t.Fatal("missed event must be claimable")
	}
	if err := s.Ack("c1", e3.Seq); err != nil {
		t.Fatal(err)
	}
	if rest := s.Replay("c1", 100, Filter{}); len(rest) != 0 {
		t.Fatalf("replay must converge to empty, got %d", len(rest))
	}
}

// TestSubscribeDurableDedupes proves the replay/live overlap is safe: an
// event delivered both by replay and live is processed once.
func TestSubscribeDurableDedupes(t *testing.T) {
	s := openReplayStream(t)
	e := publishDurable(t, s, MemoryCreated, "r9")

	var processed []string
	unsub := s.SubscribeDurable("c9", Filter{}, func(ev *Event) error {
		processed = append(processed, ev.ID)
		return nil
	})
	defer unsub()

	// The live path re-delivers the same event (as a fresh publish of the
	// same logical effect would via redelivery): process must stay at 1.
	s.deliver(&Event{ID: e.ID, Type: MemoryCreated, RequestID: "r9", Seq: e.Seq})
	if len(processed) != 1 {
		t.Fatalf("event processed %d times, want 1", len(processed))
	}
	if got := s.Checkpoint("c9"); got != e.Seq {
		t.Fatalf("checkpoint must advance to %d, got %d", e.Seq, got)
	}
}

// TestFailedHandlerRedelivers proves a handler failure does not ack, so
// the event comes back on the next replay instead of dropping.
func TestFailedHandlerRedelivers(t *testing.T) {
	s := openReplayStream(t)
	e := publishDurable(t, s, MemoryCreated, "r7")

	calls := 0
	unsub := s.SubscribeDurable("c7", Filter{}, func(ev *Event) error {
		calls++
		// Fail the first attempt only by tracking call count outside the
		// claim: the claim is consumed once, so redelivery needs a fresh
		// look — here we assert the failure did NOT ack.
		if calls == 1 {
			return errSimulated
		}
		return nil
	})
	defer unsub()

	if calls != 1 {
		t.Fatalf("expected 1 live call, got %d", calls)
	}
	if got := s.Checkpoint("c7"); got != 0 {
		t.Fatalf("failed handler must not advance checkpoint, got %d", got)
	}
	// The event is still unacked: a post-restart replay returns it.
	rest := s.Replay("c7", 100, Filter{})
	if len(rest) != 1 || rest[0].ID != e.ID {
		t.Fatalf("failed event must redeliver, got %d", len(rest))
	}
	// And a restarted subscriber actually reprocesses it (the failed
	// claim was released, so this is not swallowed as a duplicate).
	unsub2 := s.SubscribeDurable("c7", Filter{}, func(ev *Event) error {
		calls++
		return nil
	})
	defer unsub2()
	if calls != 2 {
		t.Fatalf("restart must reprocess the failed event, calls=%d", calls)
	}
	if got := s.Checkpoint("c7"); got != e.Seq {
		t.Fatalf("checkpoint must advance after success, got %d", got)
	}
}
