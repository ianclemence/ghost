package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// liveContext is the real request shape: the owner's own words plus the
// validated device timezone.
func liveContext(t *testing.T, tz, ownerMessage string) context.Context {
	t.Helper()
	ctx := context.Background()
	ctx = WithRequestTimezone(ctx, tz)
	return WithRequestMessage(ctx, ownerMessage)
}

func scheduledItems(svc *fakeScheduleService) []*scheduled.ScheduledItem {
	var out []*scheduled.ScheduledItem
	for _, it := range svc.items {
		if it.State == scheduled.StateScheduled {
			out = append(out, it)
		}
	}
	return out
}

// The live failure, reproduced: the owner said "a day before" and the model
// supplied a time instead. Nothing may be stored until the owner names a day.
func TestScheduleRefusesAModelInventedTime(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(liveContext(t, "Asia/Bangkok", "remind me about the chelsea game a day before"),
		map[string]interface{}{
			"message": "Remind me tomorrow at 9 AM about the chelsea game",
		})
	if !res.IsError {
		t.Fatalf("a time the owner never named must not be stored; got %+v", res.ForLLM)
	}
	if got := len(scheduledItems(svc)); got != 0 {
		t.Fatalf("nothing may be scheduled, found %d item(s)", got)
	}
	// The question must quote what the owner actually said, so they know what
	// to correct.
	if !strings.Contains(res.ForLLM, "before") {
		t.Fatalf("the question must name the phrase that could not be resolved, got %q", res.ForLLM)
	}
}

// The same request, with the day the owner now states: it resolves to that
// date — the thing that was impossible before.
func TestScheduleStoresTheOwnersDate(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(liveContext(t, "Asia/Bangkok", "remind me about the chelsea game on 9 october at 9 am"),
		map[string]interface{}{
			"message": "Remind me on 9 October at 9 AM about the chelsea game",
		})
	if res.IsError {
		t.Fatalf("the owner named a real date and it was refused: %s", res.ForLLM)
	}
	items := scheduledItems(svc)
	if len(items) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(items))
	}
	loc, _ := time.LoadLocation("Asia/Bangkok")
	want := time.Date(2026, 10, 9, 9, 0, 0, 0, loc)
	if items[0].NextRunAt == nil || !items[0].NextRunAt.Equal(want) {
		t.Fatalf("stored %v, want %v", items[0].NextRunAt, want)
	}
}

// The owner's own words outrank the model's restatement when they disagree.
func TestSchedulePrefersTheOwnersWords(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(liveContext(t, "Asia/Bangkok", "remind me friday at 9 am to call Sam"),
		map[string]interface{}{
			// The model drifted to a different day and hour.
			"message": "Remind me tomorrow at 8 AM to call Sam",
		})
	if res.IsError {
		t.Fatalf("the owner named a time and it was refused: %s", res.ForLLM)
	}
	items := scheduledItems(svc)
	if len(items) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(items))
	}
	loc, _ := time.LoadLocation("Asia/Bangkok")
	// Saturday 26 September 2026 → the next Friday is 2 October, at 09:00.
	want := time.Date(2026, 10, 2, 9, 0, 0, 0, loc)
	if items[0].NextRunAt == nil || items[0].NextRunAt.Equal(want) == false {
		// Allow the current week's Friday when the test runs on another date:
		// the point is that it is a Friday at 09:00, not the model's choice.
		if items[0].NextRunAt == nil || items[0].NextRunAt.Weekday() != time.Friday || items[0].NextRunAt.Hour() != 9 {
			t.Fatalf("stored %v, want a Friday at 09:00 (the owner's words), not the model's", items[0].NextRunAt)
		}
	}
}

// A bare yes to a time Ghost already proposed is agreement, not invention.
func TestScheduleAcceptsAShortConfirmation(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(liveContext(t, "Asia/Bangkok", "yes"),
		map[string]interface{}{
			"message": "Remind me on 9 October at 9 AM about the chelsea game",
		})
	if res.IsError {
		t.Fatalf("a confirmation of a proposed time must be accepted: %s", res.ForLLM)
	}
	if got := len(scheduledItems(svc)); got != 1 {
		t.Fatalf("expected 1 reminder, got %d", got)
	}
}

// No time at all: ask, do not choose.
func TestScheduleAsksWhenTheOwnerGaveNoTime(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(liveContext(t, "Asia/Bangkok", "remind me to call Sam"),
		map[string]interface{}{"message": "Remind me tomorrow at 9 AM to call Sam"})
	if !res.IsError {
		t.Fatal("a reminder with no time from the owner must not be scheduled silently")
	}
	if got := len(scheduledItems(svc)); got != 0 {
		t.Fatalf("nothing may be scheduled, found %d", got)
	}
	// The accepted shapes must be named so the model can retry from them.
	for _, want := range []string{"tomorrow at 9 AM", "9 October at 9 AM"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Fatalf("the ask must name the shape %q, got %q", want, res.ForLLM)
		}
	}
}

// Direct tool use without an owner message keeps the previous behaviour, so
// nothing outside the chat path changes.
func TestScheduleWithoutRequestMessageUsesSuppliedPhrasing(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(context.Background(), map[string]interface{}{
		"message": "Remind me on 9 October at 9 AM to call Sam",
	})
	if res.IsError {
		t.Fatalf("direct tool use must keep working: %s", res.ForLLM)
	}
	if got := len(scheduledItems(svc)); got != 1 {
		t.Fatalf("expected 1 reminder, got %d", got)
	}
}

func TestOwnerConfirmationDetection(t *testing.T) {
	yes := []string{"yes", "yeah", "ok", "sure", "please do", "go ahead", "Confirmed", "sounds good"}
	for _, m := range yes {
		if !isShortConfirmation(m) {
			t.Errorf("isShortConfirmation(%q) = false, want true", m)
		}
	}
	no := []string{
		"remind me about the chelsea game a day before",
		"no just set the reminder",
		"remind me to call Sam",
		"set it for 9 october",
		"yes on friday",
		"",
	}
	for _, m := range no {
		if isShortConfirmation(m) {
			t.Errorf("isShortConfirmation(%q) = true, want false", m)
		}
	}
}

// An approved reminder re-runs with the approval reply as the turn text. That
// is consent to the schedule the owner was shown, not a fresh instruction, and
// the resume must not be rejected for lacking a time.
func TestScheduleResumesOnAnApprovalReply(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	for _, reply := range []string{"allow once", "approve", "always allow", "allow"} {
		svc = newFakeScheduleService()
		tool = NewScheduleTool(svc, "Asia/Bangkok")
		tool.SetContext("mobile", "default")
		res := tool.Execute(liveContext(t, "Asia/Bangkok", reply),
			map[string]interface{}{"message": "Remind me on 9 October at 9 AM about the chelsea game"})
		if res.IsError {
			t.Fatalf("approval reply %q must resume the approved call, got: %s", reply, res.ForLLM)
		}
		items := scheduledItems(svc)
		if len(items) != 1 {
			t.Fatalf("reply %q: expected 1 reminder, got %d", reply, len(items))
		}
		loc, _ := time.LoadLocation("Asia/Bangkok")
		want := time.Date(2026, 10, 9, 9, 0, 0, 0, loc)
		if items[0].NextRunAt == nil || !items[0].NextRunAt.Equal(want) {
			t.Fatalf("reply %q: stored %v, want %v", reply, items[0].NextRunAt, want)
		}
	}
}
