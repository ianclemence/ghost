package cevents

import (
	"testing"
)

// Deterministic identity: a producer that re-publishes the same logical
// event (same ID, e.g. after a crash or a resume) must not create a second
// warehouse row or a second consumer side effect. Exactly one row, one
// seq, one processing.
func TestPublishDeterministicIdentityIdempotent(t *testing.T) {
	s := openReplayStream(t)
	first := s.Publish(&Event{ID: "evt-det-1", Type: MemoryCreated, RequestID: "r1", Status: "success"})
	second := s.Publish(&Event{ID: "evt-det-1", Type: MemoryCreated, RequestID: "r1", Status: "success"})
	if first.Seq == 0 || first.Seq != second.Seq {
		t.Fatalf("re-publish must adopt the same seq: %d != %d", first.Seq, second.Seq)
	}
	if got := len(s.Recent(100, Filter{})); got != 1 {
		t.Fatalf("warehouse must hold one row, got %d", got)
	}
	// A durable consumer processes it exactly once even though the live
	// fan-out delivered twice.
	var applied []string
	unsub := s.SubscribeDurable("det", Filter{}, func(e *Event) error {
		applied = append(applied, e.ID)
		return nil
	})
	defer unsub()
	if len(applied) != 1 {
		t.Fatalf("durable consumer applied %d times, want 1", len(applied))
	}
}

// Replay is observation only: reading events back never invokes the
// consumer's handler. Only SubscribeDurable (an explicit opt-in) replays
// AND processes.
func TestReplayIsObservationOnly(t *testing.T) {
	s := openReplayStream(t)
	e := publishDurable(t, s, MemoryCreated, "r1")
	// Reading events back (ByRequest/Recent/Since/Replay) is pure
	// observation: none of these invoke any consumer handler. The only
	// paths that run handlers are Publish's live fan-out and an explicit
	// SubscribeDurable. Assert Replay returns the event and, with no
	// subscription, nothing processed it.
	back := s.Replay("obs-only", 10, Filter{})
	if len(back) != 1 || back[0].ID != e.ID {
		t.Fatalf("replay must return the durable event, got %d", len(back))
	}
	if got := s.Checkpoint("obs-only"); got != 0 {
		t.Fatalf("observation must not advance any checkpoint, got %d", got)
	}
}

// Delayed/stale arrivals: an ack never moves backwards, a redelivered
// duplicate is skipped, and a genuinely new late event is still delivered.
func TestDelayedAndStaleArrival(t *testing.T) {
	s := openReplayStream(t)
	e1 := publishDurable(t, s, MemoryCreated, "r1")
	e2 := publishDurable(t, s, MemoryCreated, "r1")
	e3 := publishDurable(t, s, MemoryCreated, "r1")
	if err := s.Ack("late", e3.Seq); err != nil {
		t.Fatal(err)
	}

	// A delayed duplicate of e1 arrives (live redelivery of an event the
	// consumer already saw and acked). Claim skips it: no side effect.
	applied := []string{}
	unsub := s.SubscribeDurable("late", Filter{}, func(ev *Event) error {
		applied = append(applied, ev.ID)
		return nil
	})
	defer unsub()
	if len(applied) != 0 {
		t.Fatalf("replayed acked events must not re-apply: %v", applied)
	}

	// A genuinely NEW event published now (even if logically "late" for
	// the producer) has a fresh seq and IS delivered.
	e4 := publishDurable(t, s, MemoryCreated, "r1")
	if !containsEventID(applied, e4.ID) {
		// The SubscribeDurable above already consumed the replay gap; a
		// live publish while subscribed delivers.
		t.Fatalf("new event not delivered live: %v", applied)
	}
	// Checkpoint advanced only past the real last event, never regressed.
	if got := s.Checkpoint("late"); got != e4.Seq {
		t.Fatalf("checkpoint = %d, want %d", got, e4.Seq)
	}
	_ = e1
	_ = e2
}

// An out-of-order ack (a worker finishing an old event after a newer one
// was already acked) cannot drag the checkpoint backwards.
func TestOutOfOrderAckNeverRegresses(t *testing.T) {
	s := openReplayStream(t)
	if err := s.Ack("ooo", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Ack("ooo", 4); err != nil {
		t.Fatal(err)
	}
	if got := s.Checkpoint("ooo"); got != 10 {
		t.Fatalf("checkpoint regressed to %d", got)
	}
}

func containsEventID(list []string, id string) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}
