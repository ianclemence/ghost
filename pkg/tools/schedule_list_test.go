package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

func mustCreate(t *testing.T, fake *fakeScheduleService, item *scheduled.ScheduledItem) {
	t.Helper()
	if err := fake.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
}

// action=list is the read side of scheduling: no session context, no
// message. "Check my reminders" must answer with what is still pending
// AND what already fired (regression: the agent had no way to read the
// schedule back at all — "I don't have a tool to list scheduled items").
func TestScheduleListShowsPendingAndCompleted(t *testing.T) {
	fake := newFakeScheduleService()
	next := time.Now().UTC().Add(24 * time.Hour)
	mustCreate(t, fake, &scheduled.ScheduledItem{
		ID: "p1", Type: scheduled.TypeReminder, Title: "Call Jas",
		State: scheduled.StateScheduled, Timezone: "Asia/Bangkok",
		Schedule:  scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &next},
		NextRunAt: &next,
	})
	fired := time.Now().UTC().Add(-time.Hour)
	mustCreate(t, fake, &scheduled.ScheduledItem{
		ID: "c1", Type: scheduled.TypeReminder, Title: "Email Maria",
		State: scheduled.StateCompleted, Timezone: "Asia/Bangkok",
		Schedule:  scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &fired},
		LastRunAt: &fired,
	})

	tool := NewScheduleTool(fake, "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list failed: %s", res.ForLLM)
	}
	for _, want := range []string{"Call Jas", "Email Maria", "pending", "completed"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Fatalf("list output missing %q:\n%s", want, res.ForLLM)
		}
	}
}

// An empty schedule must say so explicitly — an honest "nothing is set"
// is exactly what lets the model distinguish "already fired / none set"
// from "I can't read the schedule".
func TestScheduleListEmptyIsExplicit(t *testing.T) {
	tool := NewScheduleTool(newFakeScheduleService(), "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Nothing is scheduled") {
		t.Fatalf("empty list must be explicit, got: %s", res.ForLLM)
	}
}

// Listing needs neither session context nor a message: the read branch
// runs before both guards.
func TestScheduleListNeedsNoSessionContext(t *testing.T) {
	tool := NewScheduleTool(newFakeScheduleService(), "Asia/Bangkok") // no SetContext
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list without session context failed: %s", res.ForLLM)
	}
}

// Failed items surface with their error so "check them" can also explain
// a reminder that never went off.
func TestScheduleListShowsFailed(t *testing.T) {
	fake := newFakeScheduleService()
	past := time.Now().UTC().Add(-time.Hour)
	mustCreate(t, fake, &scheduled.ScheduledItem{
		ID: "f1", Type: scheduled.TypeReminder, Title: "Check Ghost logs",
		State: scheduled.StateFailed, Timezone: "Asia/Bangkok",
		Schedule:  scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &past},
		LastRunAt: &past,
		LastError: "approval expired",
	})
	tool := NewScheduleTool(fake, "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "approval expired") {
		t.Fatalf("failed item error missing:\n%s", res.ForLLM)
	}
}
