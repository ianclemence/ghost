package computer

import (
	"time"
)

// Evidence describes what execution observed, in safe metadata only:
// operation, resource, task/session/context, timestamps, outcome, and
// references (screenshots by ID, files by hash). Never credentials,
// never keystrokes of secrets, never raw page text containing secrets.
// Callers publish these through the canonical event stream.
type Evidence struct {
	Operation  Op
	ResourceID string
	TaskID     string
	SessionKey string
	ContextID  string
	StartedAt  time.Time
	EndedAt    time.Time
	Outcome    string
	Detail     string
	Refs       map[string]string
}

// Outcomes for computer evidence.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
	OutcomeDenied  = "denied"
	OutcomeBusy    = "busy"
	OutcomeOffline = "offline"
)

// BeginEvidence starts an evidence record for an operation.
func BeginEvidence(op Op, resourceID, taskID, sessionKey, contextID string) *Evidence {
	return &Evidence{
		Operation:  op,
		ResourceID: resourceID,
		TaskID:     taskID,
		SessionKey: sessionKey,
		ContextID:  contextID,
		StartedAt:  time.Now().UTC(),
		Refs:       map[string]string{},
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
		"operation":   string(e.Operation),
		"resource_id": e.ResourceID,
		"task_id":     e.TaskID,
		"session_key": e.SessionKey,
		"context_id":  e.ContextID,
		"started_at":  e.StartedAt.Format(time.RFC3339),
		"outcome":     e.Outcome,
		"detail":      e.Detail,
	}
	if !e.EndedAt.IsZero() {
		out["ended_at"] = e.EndedAt.Format(time.RFC3339)
	}
	for k, v := range e.Refs {
		out["ref_"+k] = v
	}
	return out
}
