package browser

import (
	"time"
)

// Evidence describes what a browser action observably did: navigation
// targets, extracted facts, outcomes. Secrets never appear here —
// observations are redacted before evidence is built, and Refs carry
// identifiers (screenshot IDs, file hashes), never content.
type Evidence struct {
	Operation  string
	SessionID  string
	TaskID     string
	ContextID  string
	URL        string
	StartedAt  time.Time
	EndedAt    time.Time
	Outcome    string
	Detail     string
	Refs       map[string]string
}

// Outcomes for browser evidence.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
	OutcomeDenied  = "denied"
	OutcomeBlocked = "blocked"
)

// BeginEvidence starts an evidence record.
func BeginEvidence(operation, sessionID, taskID, contextID, url string) *Evidence {
	return &Evidence{
		Operation: operation,
		SessionID: sessionID,
		TaskID:    taskID,
		ContextID: contextID,
		URL:       url,
		StartedAt: time.Now().UTC(),
		Refs:      map[string]string{},
	}
}

// Finish closes the record. Outcome must be one of the Outcome constants.
func (e *Evidence) Finish(outcome, detail string) *Evidence {
	e.EndedAt = time.Now().UTC()
	e.Outcome = outcome
	e.Detail = detail
	return e
}

// Payload renders the record as event-safe string metadata.
func (e *Evidence) Payload() map[string]string {
	out := map[string]string{
		"operation":  e.Operation,
		"session_id": e.SessionID,
		"task_id":    e.TaskID,
		"context_id": e.ContextID,
		"url":        e.URL,
		"started_at": e.StartedAt.Format(time.RFC3339),
		"outcome":    e.Outcome,
		"detail":     e.Detail,
	}
	if !e.EndedAt.IsZero() {
		out["ended_at"] = e.EndedAt.Format(time.RFC3339)
	}
	for k, v := range e.Refs {
		out["ref_"+k] = v
	}
	return out
}
