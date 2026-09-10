package cevents

import (
	"testing"
)

// One trajectory ID must connect a turn's events across types: publish with
// the ID, read the whole trace back in order, and filter subscriptions by it.
func TestTrajectoryLinksTrace(t *testing.T) {
	s := openTestStream(t)
	trj := "trj_test123"
	types := []Type{MessageReceived, CapabilityStarted, ToolCompleted, AgentCompleted}
	for _, typ := range types {
		s.Publish(&Event{Type: typ, RequestID: "req-1", SessionID: "s-1", TrajectoryID: trj})
	}
	// An event from another trace must not leak in.
	s.Publish(&Event{Type: CapabilityStarted, RequestID: "req-2", SessionID: "s-2", TrajectoryID: "trj_other"})

	got := s.ByTrajectory(trj)
	if len(got) != len(types) {
		t.Fatalf("ByTrajectory returned %d events, want %d", len(got), len(types))
	}
	for i, typ := range types {
		if got[i].Type != typ || got[i].TrajectoryID != trj {
			t.Fatalf("position %d: got %s/%q", i, got[i].Type, got[i].TrajectoryID)
		}
		if i > 0 && got[i].Seq <= got[i-1].Seq {
			t.Fatal("trace sequence must be monotonic")
		}
	}
}

// Filter-scoped reads (Recent/Since) honor the trajectory, and live
// subscriptions match on it.
func TestTrajectoryFilter(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: CapabilityStarted, TrajectoryID: "trj_a"})
	s.Publish(&Event{Type: CapabilityStarted, TrajectoryID: "trj_b"})

	if got := s.Recent(10, Filter{TrajectoryID: "trj_a"}); len(got) != 1 {
		t.Fatalf("Recent filtered = %d, want 1", len(got))
	}
	if got := s.Since(0, 10, Filter{TrajectoryID: "trj_b"}); len(got) != 1 {
		t.Fatalf("Since filtered = %d, want 1", len(got))
	}
	var seen int
	unsub := s.Subscribe(Filter{TrajectoryID: "trj_a"}, func(*Event) { seen++ })
	s.Publish(&Event{Type: AgentCompleted, TrajectoryID: "trj_a"})
	s.Publish(&Event{Type: AgentCompleted, TrajectoryID: "trj_b"})
	unsub()
	if seen != 1 {
		t.Fatalf("subscription saw %d events, want 1", seen)
	}
}

// Pre-trajectory rows (NULL trajectory_id, empty strings elsewhere —
// exactly what Publish wrote before trajectories existed) must still scan.
func TestLegacyNullTrajectoryScans(t *testing.T) {
	s := openTestStream(t)
	if _, err := s.db.Exec(`INSERT INTO canonical_events (id, type, request_id, session_id, conversation_id, ghost_id, agent_id, routine_id, timestamp, visibility, status, payload)
		VALUES ('legacy-1', 'message.received', 'req-0', '', '', '', '', '', '2026-01-01T00:00:00Z', 'internal_trace', '', '{}')`); err != nil {
		t.Fatal(err)
	}
	got := s.ByRequest("req-0")
	if len(got) != 1 {
		t.Fatalf("legacy row unreadable: %d", len(got))
	}
	if got[0].TrajectoryID != "" {
		t.Fatalf("legacy row must have empty trajectory, got %q", got[0].TrajectoryID)
	}
}

// EnsureTrajectoryColumn converges an old-shaped table and is idempotent.
func TestEnsureTrajectoryColumnConverges(t *testing.T) {
	s := openTestStream(t)
	if _, err := s.db.Exec(`ALTER TABLE canonical_events DROP COLUMN trajectory_id`); err != nil {
		t.Skipf("sqlite build without DROP COLUMN: %v", err)
	}
	if err := EnsureTrajectoryColumn(s.db); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := EnsureTrajectoryColumn(s.db); err != nil {
		t.Fatalf("ensure must be idempotent: %v", err)
	}
	s.Publish(&Event{Type: ToolCompleted, TrajectoryID: "trj_x"})
	if got := s.ByTrajectory("trj_x"); len(got) != 1 {
		t.Fatal("publish after converge must be queryable")
	}
}
