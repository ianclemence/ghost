package telemetry

import (
	"testing"
)

func TestTelemetryManager(t *testing.T) {
	m := NewManager()
	session := "test-session"
	reqID := "req-1"
	
	// Test recording
	m.Record(session, reqID, "queued", "cli", "direct", "")
	m.Record(session, reqID, "processing", "cli", "direct", "")
	
	// Test retrieval by ID
	traces := m.GetTraces(reqID)
	if len(traces) != 2 {
		t.Errorf("expected 2 traces, got %d", len(traces))
	}
	if traces[0].State != "queued" {
		t.Errorf("expected first state to be queued, got %s", traces[0].State)
	}
	
	// Test retrieval by session
	lastReq := m.GetLastRequestID(session)
	if lastReq != reqID {
		t.Errorf("expected last request ID to be %s, got %s", reqID, lastReq)
	}
	
	// Test trace by session
	id, sTraces := m.GetTraceBySession(session)
	if id != reqID {
		t.Errorf("expected session request ID to be %s, got %s", reqID, id)
	}
	if len(sTraces) != 2 {
		t.Errorf("expected 2 session traces, got %d", len(sTraces))
	}
	
	// Test incidents
	m.RecordIncident("telegram", "auth error")
	incidents := m.GetIncidents()
	if len(incidents) != 1 {
		t.Errorf("expected 1 incident, got %d", len(incidents))
	}
	if incidents["telegram"].LastError != "auth error" {
		t.Errorf("expected incident error 'auth error', got %s", incidents["telegram"].LastError)
	}
	
	m.ClearIncidents("telegram")
	if len(m.GetIncidents()) != 0 {
		t.Error("expected 0 incidents after clear")
	}
}
