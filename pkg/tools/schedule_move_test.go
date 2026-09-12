package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// fakeScheduleService is a minimal in-memory Scheduler: create, list,
// cancel. It intentionally implements the full surface the reschedule
// flow needs.
type fakeScheduleService struct {
	items map[string]*scheduled.ScheduledItem
}

func newFakeScheduleService() *fakeScheduleService {
	return &fakeScheduleService{items: map[string]*scheduled.ScheduledItem{}}
}

func (f *fakeScheduleService) CreateItem(item *scheduled.ScheduledItem) error {
	if item.ID == "" {
		item.ID = "test-id"
	}
	cp := *item
	f.items[item.ID] = &cp
	return nil
}

func (f *fakeScheduleService) ListItems(_ scheduled.ItemType, state scheduled.ItemState, _ int) ([]*scheduled.ScheduledItem, error) {
	var out []*scheduled.ScheduledItem
	for _, it := range f.items {
		if state != "" && it.State != state {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (f *fakeScheduleService) CancelItem(id string) error {
	it, ok := f.items[id]
	if !ok {
		return nil
	}
	it.State = scheduled.StateCancelled
	return nil
}

// The confirmation must carry the exact stored time including minutes:
// "Saturday at 7:45 PM", never a rounded "Saturday at 7 PM".
func TestFormatScheduleKeepsMinutes(t *testing.T) {
	at := time.Date(2026, 9, 12, 19, 45, 0, 0, time.UTC)
	parsed := &scheduled.ParsedSchedule{
		Schedule:  scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &at},
		Title:     "Today at 7:45 PM",
		Timezone:  "UTC",
		IsOneTime: true,
	}
	got := formatScheduleForUser(parsed)
	if !strings.Contains(got, "7:45") {
		t.Fatalf("confirmation dropped minutes: %q", got)
	}
}

// Moving a reminder cancels the old item and creates exactly one new one.
func TestRescheduleCancelsOldReminder(t *testing.T) {
	svc := newFakeScheduleService()
	oldAt := time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC)
	svc.items["old-1"] = &scheduled.ScheduledItem{
		ID:          "old-1",
		Type:        scheduled.TypeReminder,
		Title:       "Today at 9 PM",
		Description: "Remind me today at 9pm that Chelsea is playing",
		State:       scheduled.StateScheduled,
		Timezone:    "Asia/Bangkok",
		Channel:     "mobile",
		ChatID:      "default",
		Schedule:    scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &oldAt},
		Action:      scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: "Chelsea is playing now — time to watch the match!"},
	}

	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")
	res := tool.Execute(context.Background(), map[string]interface{}{
		"message":    "Remind me today at 7:45 PM to watch Chelsea vs Hull City",
		"reschedule": "the 9pm Chelsea reminder",
	})
	if res.IsError {
		t.Fatalf("reschedule failed: %s", res.ForLLM)
	}
	if svc.items["old-1"].State != scheduled.StateCancelled {
		t.Fatalf("old reminder state = %q, want cancelled", svc.items["old-1"].State)
	}
	live := 0
	for _, it := range svc.items {
		if it.State == scheduled.StateScheduled {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("live scheduled items = %d, want exactly 1 (move, not copy)", live)
	}
	if !strings.Contains(res.ForLLM, "Moved") {
		t.Errorf("confirmation should say it moved, got: %q", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "7:45") {
		t.Errorf("confirmation should carry exact new time, got: %q", res.ForLLM)
	}
}

// When nothing matches the reschedule description, a fresh item is
// created (never a wrong cancellation).
func TestRescheduleNoMatchCreatesFresh(t *testing.T) {
	svc := newFakeScheduleService()
	tool := NewScheduleTool(svc, "Asia/Bangkok")
	tool.SetContext("mobile", "default")
	res := tool.Execute(context.Background(), map[string]interface{}{
		"message":    "Remind me tomorrow at 8 AM to drink water",
		"reschedule": "the dentist appointment next spring",
	})
	if res.IsError {
		t.Fatalf("fresh create failed: %s", res.ForLLM)
	}
	if len(svc.items) != 1 {
		t.Fatalf("items = %d, want 1 fresh item", len(svc.items))
	}
}
