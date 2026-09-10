package cevents

import (
	"time"
)

// TrajectorySummary is a replayable description of one execution trace. It is
// what `ghost replay <trajectory-id>` prints: the model/effort/tools/
// verification/fallback sequence and the terminal outcome, reconstructed from
// durable events. Replay describes; it never re-executes.
type TrajectorySummary struct {
	TrajectoryID  string           `json:"trajectory_id"`
	SessionID     string           `json:"session_id,omitempty"`
	RequestID     string           `json:"request_id,omitempty"`
	StartedAt     time.Time        `json:"started_at,omitempty"`
	EndedAt       time.Time        `json:"ended_at,omitempty"`
	DurationMs    int64            `json:"duration_ms"`
	Outcome       string           `json:"outcome,omitempty"`
	Effort        string           `json:"effort,omitempty"`
	Models        []string         `json:"models,omitempty"`
	Tools         []string         `json:"tools,omitempty"`
	Verifications []string         `json:"verifications,omitempty"`
	Fallbacks     []string         `json:"fallbacks,omitempty"`
	Errors        []string         `json:"errors,omitempty"`
	Steps         []TrajectoryStep `json:"steps"`
}

// TrajectoryStep is one ordered event in the trace.
type TrajectoryStep struct {
	Seq       int64                  `json:"seq"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// DescribeTrajectory reconstructs a trace from durable events, in sequence
// order. It returns nil when the trajectory has no events.
func (s *Stream) DescribeTrajectory(trajectoryID string) *TrajectorySummary {
	events := s.ByTrajectory(trajectoryID)
	if len(events) == 0 {
		return nil
	}
	sum := &TrajectorySummary{TrajectoryID: trajectoryID}
	seen := map[string]map[string]bool{}
	add := func(kind, v string) {
		if v == "" {
			return
		}
		if seen[kind] == nil {
			seen[kind] = map[string]bool{}
		}
		if seen[kind][v] {
			return
		}
		seen[kind][v] = true
		switch kind {
		case "model":
			sum.Models = append(sum.Models, v)
		case "tool":
			sum.Tools = append(sum.Tools, v)
		case "verification":
			sum.Verifications = append(sum.Verifications, v)
		case "fallback":
			sum.Fallbacks = append(sum.Fallbacks, v)
		case "error":
			sum.Errors = append(sum.Errors, v)
		}
	}
	for _, e := range events {
		sum.Steps = append(sum.Steps, TrajectoryStep{
			Seq: e.Seq, Type: string(e.Type), Status: e.Status,
			Timestamp: e.Timestamp, Payload: e.Payload,
		})
		if sum.SessionID == "" {
			sum.SessionID = e.SessionID
		}
		if sum.RequestID == "" {
			sum.RequestID = e.RequestID
		}
		if sum.StartedAt.IsZero() || e.Timestamp.Before(sum.StartedAt) {
			sum.StartedAt = e.Timestamp
		}
		if e.Timestamp.After(sum.EndedAt) {
			sum.EndedAt = e.Timestamp
		}
		switch e.Type {
		case AgentCompleted:
			sum.Outcome = "success"
		case AgentFailed:
			sum.Outcome = "failed"
		case AgentWaiting:
			sum.Outcome = "waiting"
		case EffortSelected:
			if lvl, _ := e.Payload["level"].(string); lvl != "" {
				sum.Effort = lvl
			}
		case ToolCompleted, ToolFailed:
			if t, _ := e.Payload["tool"].(string); t != "" {
				add("tool", t)
			}
			if e.Status == "failed" {
				add("error", "tool:"+str(e.Payload["tool"]))
			}
		case VerificationCompleted:
			add("verification", "ok:"+str(e.Payload["tool"]))
		case VerificationFailed:
			add("verification", "failed:"+str(e.Payload["tool"]))
			add("error", "verification:"+str(e.Payload["tool"]))
		case FallbackStarted, ModelEscalated:
			add("fallback", str(e.Payload["from"])+"->"+str(e.Payload["to"]))
		}
	}
	if !sum.StartedAt.IsZero() && !sum.EndedAt.IsZero() {
		sum.DurationMs = sum.EndedAt.Sub(sum.StartedAt).Milliseconds()
	}
	return sum
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// RecentTrajectories returns the distinct trajectory IDs with events, newest
// first (bounded), for a `ghost replay --list` style view.
func (s *Stream) RecentTrajectories(limit int) []string {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT trajectory_id, MAX(seq) AS last FROM canonical_events
		WHERE trajectory_id IS NOT NULL AND trajectory_id != ''
		GROUP BY trajectory_id ORDER BY last DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		var last int64
		if rows.Scan(&id, &last) == nil {
			out = append(out, id)
		}
	}
	return out
}
