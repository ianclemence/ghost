package agent

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cevents"
)

// TaskOutcome is the runtime's authoritative count of the consequential work a
// turn performed. The response layer consumes it so a multi-step result is
// never flattened into binary success/failure, and the model can never invent
// the count: it comes from canonical events, not prose.
type TaskOutcome struct {
	Completed int // successful evidence-requiring calls
	Failed    int // failed calls and permission denials
	Blocked   int // consequential requests still awaiting approval
}

// Total is the number of consequential actions the turn attempted.
func (o TaskOutcome) Total() int { return o.Completed + o.Failed + o.Blocked }

// Partial reports a genuinely mixed result: some work landed and some did not.
func (o TaskOutcome) Partial() bool {
	return o.Completed > 0 && (o.Failed > 0 || o.Blocked > 0)
}

// AllFailed reports that every attempted consequential action failed or is
// blocked — the unambiguous false-completion case.
func (o TaskOutcome) AllFailed() bool { return o.Completed == 0 && (o.Failed > 0 || o.Blocked > 0) }

// Summary renders the outcome outcome-first and concisely.
func (o TaskOutcome) Summary() string {
	parts := []string{fmt.Sprintf("%d of %d completed", o.Completed, o.Total())}
	if o.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked on approval", o.Blocked))
	}
	if o.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", o.Failed))
	}
	return strings.Join(parts, ", ") + "."
}

// outcomeFromEvents counts consequential (evidence-requiring) outcomes from a
// turn's canonical events. An approval request is Blocked only while it stays
// unanswered: once the same capability completes or fails in the turn, the
// request is not double-counted.
func outcomeFromEvents(events []*cevents.Event) TaskOutcome {
	var o TaskOutcome
	openRequests := map[string]int{}
	for _, e := range events {
		if e == nil || e.Payload == nil {
			continue
		}
		capID, _ := e.Payload["capability"].(string)
		if capID == "" {
			continue
		}
		if spec, ok := capability.Get(capID); !ok || !spec.RequiresEvidence() {
			continue
		}
		switch e.Type {
		case cevents.ToolCompleted:
			if e.Status == "success" {
				o.Completed++
			}
		case cevents.ToolFailed, cevents.PermissionDenied:
			o.Failed++
		case cevents.PermissionRequested:
			openRequests[capID]++
		}
	}
	// A request still unanswered at turn end is blocked; one that was later
	// answered by a completed/failed call is not.
	answered := map[string]bool{}
	for _, e := range events {
		if e == nil || e.Payload == nil {
			continue
		}
		capID, _ := e.Payload["capability"].(string)
		if capID == "" {
			continue
		}
		if e.Type == cevents.ToolCompleted || e.Type == cevents.ToolFailed || e.Type == cevents.PermissionDenied {
			answered[capID] = true
		}
	}
	for capID, n := range openRequests {
		if answered[capID] {
			continue
		}
		o.Blocked += n
	}
	return o
}

// turnOutcome scans this turn's canonical events for the authoritative
// consequential outcome.
func (al *AgentLoop) turnOutcome(requestID string) TaskOutcome {
	if al == nil || al.governance == nil || al.governance.Events == nil || requestID == "" {
		return TaskOutcome{}
	}
	return outcomeFromEvents(al.governance.Events.ByRequest(requestID))
}

// applyPartialOutcome appends the runtime's authoritative outcome line when the
// turn partially succeeded and the reply does not already carry the counts.
// A fully successful or fully failed turn is untouched here (the completion
// gate owns the all-failed case).
func applyPartialOutcome(reply string, o TaskOutcome) (string, string) {
	if !o.Partial() {
		return reply, ""
	}
	summary := o.Summary()
	if strings.Contains(reply, summary) {
		return reply, ""
	}
	return strings.TrimRight(reply, "\n") + "\n\nRuntime: " + summary,
		"partial completion reported from runtime state"
}
