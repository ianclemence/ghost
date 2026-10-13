package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Two routines share the word "brief": the daily Morning brief and the
// Monday weekly brief-and-weather. Cancelling "weekly brief" must refuse
// (both match on "brief"); the exact full title must single one out.
// Regression: the owner approved the same cancel three times and nothing
// was ever cancelled, because the retry could never disambiguate.
func twoBriefs(t *testing.T) *fakeScheduleService {
	t.Helper()
	fake := newFakeScheduleService()
	mustCreate(t, fake, &scheduled.ScheduledItem{
		ID: "routine-morning", Type: scheduled.TypeReminder, Title: "Morning brief",
		State: scheduled.StateScheduled, Timezone: "Asia/Bangkok",
		Schedule: scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "30 8 * * *"},
	})
	mustCreate(t, fake, &scheduled.ScheduledItem{
		ID: "routine-weekly", Type: scheduled.TypeReminder, Title: "Give me a short brief of my week and the weather",
		State: scheduled.StateScheduled, Timezone: "Asia/Bangkok",
		Schedule: scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 8 * * 1"},
	})
	return fake
}

func TestCancelFuzzyBriefRefusesAndListsIDs(t *testing.T) {
	tool := NewScheduleTool(twoBriefs(t), "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "cancel", "target": "weekly brief"})
	if !res.IsError {
		t.Fatalf("fuzzy cancel must refuse, got: %s", res.ForLLM)
	}
	for _, want := range []string{"Morning brief", "week and the weather", "routine-morning", "routine-weekly", "id"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Fatalf("ambiguous cancel must list both items with ids, missing %q:\n%s", want, res.ForLLM)
		}
	}
}

func TestCancelExactTitleSinglesItOut(t *testing.T) {
	fake := twoBriefs(t)
	tool := NewScheduleTool(fake, "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{
		"action": "cancel", "target": "Give me a short brief of my week and the weather",
	})
	if res.IsError {
		t.Fatalf("exact-title cancel failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "week and the weather") {
		t.Fatalf("confirmation names the cancelled item, got: %s", res.ForLLM)
	}
	if fake.items["routine-weekly"].State != scheduled.StateCancelled {
		t.Fatalf("weekly state = %q, want cancelled", fake.items["routine-weekly"].State)
	}
	if fake.items["routine-morning"].State != scheduled.StateScheduled {
		t.Fatalf("morning state = %q, want still scheduled", fake.items["routine-morning"].State)
	}
}

func TestCancelByIDAlwaysWorks(t *testing.T) {
	fake := twoBriefs(t)
	tool := NewScheduleTool(fake, "Asia/Bangkok")
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "cancel", "target": "routine-morning"})
	if res.IsError {
		t.Fatalf("id cancel failed: %s", res.ForLLM)
	}
	if fake.items["routine-morning"].State != scheduled.StateCancelled {
		t.Fatalf("morning state = %q, want cancelled", fake.items["routine-morning"].State)
	}
}

// The reschedule path takes the same deal: an exact old title beats a
// close fuzzy neighbour.
func TestMatchScheduleQueryPrefersExactTitle(t *testing.T) {
	fake := twoBriefs(t)
	items, _ := fake.ListItems("", scheduled.StateScheduled, 100)
	got := matchScheduleQuery(items, "Give me a short brief of my week and the weather")
	if got == nil || got.ID != "routine-weekly" {
		t.Fatalf("exact title matched %+v, want routine-weekly", got)
	}
}
