package scheduled

import (
	"testing"
	"time"
)

func TestScheduledItemTypes(t *testing.T) {
	types := []ItemType{TypeReminder, TypeEvent, TypeAutomation, TypeTask}
	for _, typ := range types {
		if !ValidTypes[typ] {
			t.Errorf("expected %s to be a valid type", typ)
		}
	}
}

func TestScheduledItemStates(t *testing.T) {
	states := []ItemState{
		StateScheduled, StateDue, StateRunning, StateCompleted,
		StateFailed, StateCancelled, StateMissed, StatePaused,
	}
	for _, state := range states {
		if !ValidStates[state] {
			t.Errorf("expected %s to be a valid state", state)
		}
	}
}

func TestScheduledItemIsRecurring(t *testing.T) {
	cases := []struct {
		schedule Schedule
		want     bool
	}{
		{Schedule{Kind: ScheduleAt}, false},
		{Schedule{Kind: ScheduleEvery}, true},
		{Schedule{Kind: ScheduleCron}, true},
		{Schedule{Kind: ScheduleNone}, false},
	}
	for _, tc := range cases {
		item := &ScheduledItem{Schedule: tc.schedule}
		if got := item.IsRecurring(); got != tc.want {
			t.Errorf("IsRecurring() = %v, want %v", got, tc.want)
		}
	}
}

func TestScheduledItemIsOneTime(t *testing.T) {
	cases := []struct {
		schedule Schedule
		want     bool
	}{
		{Schedule{Kind: ScheduleAt}, true},
		{Schedule{Kind: ScheduleEvery}, false},
		{Schedule{Kind: ScheduleCron}, false},
	}
	for _, tc := range cases {
		item := &ScheduledItem{Schedule: tc.schedule}
		if got := item.IsOneTime(); got != tc.want {
			t.Errorf("IsOneTime() = %v, want %v", got, tc.want)
		}
	}
}

func TestScheduledItemHumanSchedule(t *testing.T) {
	cases := []struct {
		schedule Schedule
		want     string
	}{
		{Schedule{Kind: ScheduleAt, At: timePtr(time.Date(2025, 9, 8, 8, 0, 0, 0, time.UTC))}, "Monday, September 8 at 8:00 AM"},
		{Schedule{Kind: ScheduleEvery, Every: 24 * time.Hour}, "Every day"},
		{Schedule{Kind: ScheduleEvery, Every: time.Hour}, "Every hour"},
		{Schedule{Kind: ScheduleCron, Expr: "0 8 * * 1"}, "Every Monday at 8:00 AM"},
		{Schedule{Kind: ScheduleNone}, "Manual"},
	}
	for _, tc := range cases {
		item := &ScheduledItem{Schedule: tc.schedule}
		if got := item.HumanSchedule(); got != tc.want {
			t.Errorf("HumanSchedule() = %q, want %q", got, tc.want)
		}
	}
}

func TestItemStateTransitions(t *testing.T) {
	item := &ScheduledItem{State: StateScheduled}

	// Can execute from scheduled state
	if !item.CanExecute() {
		t.Error("expected CanExecute to be true for scheduled state")
	}

	// Transition to running
	item.State = StateRunning
	if item.CanExecute() {
		t.Error("expected CanExecute to be false for running state")
	}

	// Transition to completed
	item.State = StateCompleted
	if item.CanExecute() {
		t.Error("expected CanExecute to be false for completed state")
	}

	// Paused state
	item.State = StatePaused
	if item.IsPaused() != true {
		t.Error("expected IsPaused to be true for paused state")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "Every few seconds"},
		{time.Minute, "Every minute"},
		{5 * time.Minute, "Every 5 minutes"},
		{time.Hour, "Every hour"},
		{3 * time.Hour, "Every 3 hours"},
		{24 * time.Hour, "Every day"},
		{7 * 24 * time.Hour, "Every week"},
		{30 * 24 * time.Hour, "Every 30 days"},
	}
	for _, tc := range cases {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatCronExpression(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"0 8 * * *", "Every day at 8:00 AM"},
		{"0 9 * * 1-5", "Weekdays at 9:00 AM"},
		{"0 0 * * *", "Every day at midnight"},
		{"0 */2 * * *", "Every 2 hours"},
		{"*/15 * * * *", "Every 15 minutes"},
		{"0 8 * * 1", "Every Monday at 8:00 AM"},
		{"0 8 * * 5", "Every Friday at 8:00 AM"},
		{"30 7 * * 1-5", "30 7 * * 1-5"},
	}
	for _, tc := range cases {
		got := formatCronExpression(tc.expr)
		if got != tc.want {
			t.Errorf("formatCronExpression(%q) = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestExecutionRecordCreation(t *testing.T) {
	record := &ExecutionRecord{
		ID:          "test-id",
		ItemID:      "item-123",
		ExecutionID: "exec-456",
		ScheduledAt: time.Now().UTC(),
		StartedAt:   time.Now().UTC(),
		Status:      "ok",
	}

	if record.ID != "test-id" {
		t.Errorf("expected ID test-id, got %s", record.ID)
	}
	if record.ItemID != "item-123" {
		t.Errorf("expected ItemID item-123, got %s", record.ItemID)
	}
	if record.Status != "ok" {
		t.Errorf("expected Status ok, got %s", record.Status)
	}
}
