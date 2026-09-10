package cevents

import "testing"

func TestDescribeTrajectory(t *testing.T) {
	s := openTestStream(t)
	trj := "trj_replay1"
	s.Publish(&Event{Type: AgentStarted, TrajectoryID: trj, SessionID: "s1", RequestID: "r1"})
	s.Publish(&Event{Type: EffortSelected, TrajectoryID: trj, Payload: map[string]interface{}{"level": "normal"}})
	s.Publish(&Event{Type: ToolCompleted, TrajectoryID: trj, Status: "success", Payload: map[string]interface{}{"tool": "calendar_create"}})
	s.Publish(&Event{Type: VerificationFailed, TrajectoryID: trj, Status: "failed", Payload: map[string]interface{}{"tool": "calendar_create"}})
	s.Publish(&Event{Type: FallbackStarted, TrajectoryID: trj, Payload: map[string]interface{}{"from": "ollama/qwen", "to": "deepseek/deepseek-flash"}})
	s.Publish(&Event{Type: AgentCompleted, TrajectoryID: trj, Status: "success"})

	sum := s.DescribeTrajectory(trj)
	if sum == nil {
		t.Fatal("expected a summary")
	}
	if sum.Outcome != "success" || sum.Effort != "normal" {
		t.Fatalf("outcome/effort wrong: %+v", sum)
	}
	if len(sum.Tools) != 1 || sum.Tools[0] != "calendar_create" {
		t.Fatalf("tools wrong: %v", sum.Tools)
	}
	if len(sum.Verifications) != 1 || sum.Verifications[0] != "failed:calendar_create" {
		t.Fatalf("verifications wrong: %v", sum.Verifications)
	}
	if len(sum.Fallbacks) != 1 || len(sum.Errors) != 1 {
		t.Fatalf("fallbacks/errors wrong: %v / %v", sum.Fallbacks, sum.Errors)
	}
	if len(sum.Steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(sum.Steps))
	}
	// Steps are sequence-ordered.
	for i := 1; i < len(sum.Steps); i++ {
		if sum.Steps[i].Seq <= sum.Steps[i-1].Seq {
			t.Fatal("steps must be sequence-ordered")
		}
	}
	if s.DescribeTrajectory("missing") != nil {
		t.Fatal("unknown trajectory must return nil")
	}
}

func TestRecentTrajectories(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: AgentStarted, TrajectoryID: "trj_a"})
	s.Publish(&Event{Type: AgentStarted, TrajectoryID: "trj_b"})
	ids := s.RecentTrajectories(10)
	if len(ids) != 2 {
		t.Fatalf("expected 2 trajectories, got %v", ids)
	}
	// Newest (highest seq) first.
	if ids[0] != "trj_b" {
		t.Fatalf("expected newest first, got %v", ids)
	}
}
