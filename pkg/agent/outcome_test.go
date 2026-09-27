package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
)

func consequential(capability, typ, status string) *cevents.Event {
	return &cevents.Event{Type: cevents.Type(typ), Status: status, Payload: map[string]interface{}{"capability": capability}}
}

// The outcome is counted from canonical events, one step per consequential
// call, and a request that is later answered is not double-counted.
func TestOutcomeFromEventsCountsSteps(t *testing.T) {
	events := []*cevents.Event{
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
		consequential("message.send", string(cevents.PermissionRequested), "pending"),
		consequential("message.send", string(cevents.PermissionRequested), "pending"),
		// Read-only capability: never counted as consequential work.
		consequential("calendar.read", string(cevents.ToolCompleted), "success"),
	}
	o := outcomeFromEvents(events)
	if o.Completed != 3 || o.Blocked != 2 || o.Failed != 0 {
		t.Fatalf("outcome = %+v, want 3 completed / 2 blocked / 0 failed", o)
	}
	if !o.Partial() || o.AllFailed() {
		t.Fatalf("3 of 5 must be partial and not all-failed: %+v", o)
	}
	if got, want := o.Summary(), "3 of 5 completed, 2 blocked on approval."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

// A request answered later in the same turn is not also counted as blocked.
func TestOutcomeAnsweredRequestIsNotBlocked(t *testing.T) {
	events := []*cevents.Event{
		consequential("message.send", string(cevents.PermissionRequested), "pending"),
		consequential("message.send", string(cevents.ToolCompleted), "success"),
	}
	o := outcomeFromEvents(events)
	if o.Completed != 1 || o.Blocked != 0 {
		t.Fatalf("answered request must not be blocked: %+v", o)
	}
}

func TestOutcomeFailuresAreNotSuccess(t *testing.T) {
	events := []*cevents.Event{
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
		consequential("calendar.modify", string(cevents.ToolFailed), "failed"),
		consequential("message.send", string(cevents.PermissionDenied), "denied"),
	}
	o := outcomeFromEvents(events)
	if o.Completed != 1 || o.Failed != 2 || !o.Partial() {
		t.Fatalf("outcome = %+v, want partial 1/3", o)
	}
	if got, want := o.Summary(), "1 of 3 completed, 2 failed."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestOutcomeAllFailed(t *testing.T) {
	o := outcomeFromEvents([]*cevents.Event{
		consequential("calendar.modify", string(cevents.ToolFailed), "failed"),
	})
	if !o.AllFailed() || o.Partial() {
		t.Fatalf("all-failed outcome wrong: %+v", o)
	}
}

// The response layer adds the authoritative counts exactly when the result is
// partial, and never invents or repeats them.
func TestApplyPartialOutcome(t *testing.T) {
	o := outcomeFromEvents([]*cevents.Event{
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
		consequential("message.send", string(cevents.PermissionRequested), "pending"),
	})
	got, reason := applyPartialOutcome("Here's where things stand.", o)
	if reason == "" || !strings.Contains(got, "1 of 2 completed") || !strings.Contains(got, "1 blocked on approval") {
		t.Fatalf("partial outcome must be appended authoritatively, got %q reason %q", got, reason)
	}

	// A fully successful turn is untouched.
	ok := outcomeFromEvents([]*cevents.Event{
		consequential("calendar.modify", string(cevents.ToolCompleted), "success"),
	})
	if got, reason := applyPartialOutcome("Done.", ok); reason != "" || got != "Done." {
		t.Fatalf("complete turn must be untouched, got %q reason %q", got, reason)
	}

	// An already-stated summary is not duplicated.
	pre := "Here's where things stand.\n\nRuntime: " + o.Summary()
	if got, reason := applyPartialOutcome(pre, o); reason != "" || got != pre {
		t.Fatalf("existing summary must not be duplicated, got %q reason %q", got, reason)
	}
}
