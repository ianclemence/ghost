package agent

import (
	"database/sql"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	_ "modernc.org/sqlite"
)

func openTraceGovernance(t *testing.T) (*Governance, *cevents.Stream) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Governance{Events: st, GhostID: "g1", AgentID: "agent-main", seenCaps: map[string]bool{}}, st
}

// Every turn/trace event must carry the trajectory so one ID connects the
// session turn, tool execution, verification, and fallback.
func TestGovernanceEventsCarryTrajectory(t *testing.T) {
	g, st := openTraceGovernance(t)
	trj := "trj_trace1"
	g.TurnStarted("req-1", "sess-1", "mobile", trj)
	g.NoteCapability("req-1", "weather.current", trj)
	g.ToolRan("req-1", "sess-1", "weather_now", trj, false)
	g.VerificationRan("sess-1", "write_file", trj, 12, false, "")
	g.FallbackRan("req-1", "sess-1", trj, "ollama/qwen", "deepseek/chat", true)
	g.TurnEnded("req-1", "sess-1", trj, nil)

	got := st.ByTrajectory(trj)
	// Note: verification.completed + fallback/model.escalated are durable;
	// capability.started and tool/agent markers per taxonomy.
	if len(got) != 6 {
		t.Fatalf("trace must hold 6 events, got %d", len(got))
	}
	for _, e := range got {
		if e.TrajectoryID != trj {
			t.Fatalf("event %s missing trajectory", e.Type)
		}
	}
	types := map[cevents.Type]bool{}
	for _, e := range got {
		types[e.Type] = true
	}
	for _, want := range []cevents.Type{
		cevents.AgentStarted, cevents.CapabilityStarted, cevents.ToolCompleted,
		cevents.VerificationCompleted, cevents.ModelEscalated, cevents.AgentCompleted,
	} {
		if !types[want] {
			t.Fatalf("trace missing %s (have %v)", want, types)
		}
	}
}

// A failed verification is durable evidence with the tool and detail.
func TestVerificationFailedIsDurableEvidence(t *testing.T) {
	g, st := openTraceGovernance(t)
	g.VerificationRan("sess-1", "calendar_create", "trj_v", 30, true, "event not found after create")
	got := st.ByTrajectory("trj_v")
	if len(got) != 1 {
		t.Fatalf("expected 1 verification event, got %d", len(got))
	}
	e := got[0]
	if e.Type != cevents.VerificationFailed || e.Status != "failed" {
		t.Fatalf("wrong event: %s/%s", e.Type, e.Status)
	}
	if e.Payload["tool"] != "calendar_create" {
		t.Fatalf("payload must name the tool: %v", e.Payload)
	}
}

// Nil events stream stays a safe no-op (unwired loops never crash).
func TestGovernanceTraceNilSafe(t *testing.T) {
	g := &Governance{}
	g.TurnStarted("r", "s", "c", "trj")
	g.VerificationRan("s", "t", "trj", 1, false, "")
	g.FallbackRan("r", "s", "trj", "a", "b", false)
	g.TurnEnded("r", "s", "trj", nil)
}
