package scheduled

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newLedgerTestService(t *testing.T, executor func(context.Context, *ScheduledItem) error) (*Service, *Store) {
	t.Helper()
	store := NewStore(openTestDB(t))
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	return NewService(store, nil, executor), store
}

func ledgerTestItem(when time.Time) *ScheduledItem {
	return &ScheduledItem{
		Type:        TypeReminder,
		Title:       "Test reminder",
		Description: "test",
		State:       StateScheduled,
		Timezone:    "Asia/Bangkok",
		Channel:     "mobile",
		ChatID:      "default",
		Schedule:    Schedule{Kind: ScheduleAt, At: &when},
		Action:      Action{Kind: ActionAgentTurn, Content: "hello"},
		NextRunAt:   &when,
		MaxRetries:  3,
	}
}

// A successful execution must leave run_count/last_run_at incremented —
// the post-run Update must not clobber them back to zero. Uses a
// recurring item so the row survives (one-time items are deleted).
func TestExecuteItemPreservesRunCount(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	svc, _ := newLedgerTestService(t, func(context.Context, *ScheduledItem) error { return nil })
	item := ledgerTestItem(past)
	item.Type = TypeAutomation
	item.Schedule = Schedule{Kind: ScheduleEvery, Every: time.Minute}
	if err := svc.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	svc.executeItem(item)

	got, err := svc.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.RunCount != 1 {
		t.Errorf("run_count = %d, want 1", got.RunCount)
	}
	if got.LastRunAt == nil {
		t.Error("last_run_at is nil, want set")
	}
}

// The execution row must carry the channel and an honest delivery state:
// async bus turns are "dispatched", synchronous routine turns "delivered".
func TestExecuteItemDeliveryReceipts(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)

	svc, _ := newLedgerTestService(t, func(context.Context, *ScheduledItem) error { return nil })
	reminder := ledgerTestItem(past)
	if err := svc.CreateItem(reminder); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	svc.executeItem(reminder)
	hist, err := svc.GetHistory(reminder.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %d, err = %v; want 1 row", len(hist), err)
	}
	if hist[0].Channel != "mobile" {
		t.Errorf("channel = %q, want mobile", hist[0].Channel)
	}
	if hist[0].DeliveryStatus != "dispatched" {
		t.Errorf("bus-turn delivery = %q, want dispatched", hist[0].DeliveryStatus)
	}
	if hist[0].DeliveredAt != nil {
		t.Error("bus-turn delivered_at set, want nil (turn runs async)")
	}

	routine := ledgerTestItem(past)
	routine.Source = "routine"
	if err := svc.CreateItem(routine); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	svc.executeItem(routine)
	hist, err = svc.GetHistory(routine.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %d, err = %v; want 1 row", len(hist), err)
	}
	if hist[0].DeliveryStatus != "delivered" {
		t.Errorf("routine delivery = %q, want delivered", hist[0].DeliveryStatus)
	}
	if hist[0].DeliveredAt == nil {
		t.Error("routine delivered_at nil, want set")
	}
}

// Failed executions record the error and a failed delivery.
func TestExecuteItemFailureRecorded(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	svc, _ := newLedgerTestService(t, func(context.Context, *ScheduledItem) error {
		return errors.New("boom")
	})
	item := ledgerTestItem(past)
	if err := svc.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	svc.executeItem(item)
	hist, err := svc.GetHistory(item.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %d, err = %v; want 1 row", len(hist), err)
	}
	if hist[0].Status != "error" {
		t.Errorf("status = %q, want error", hist[0].Status)
	}
	if hist[0].DeliveryStatus != "failed" {
		t.Errorf("delivery = %q, want failed", hist[0].DeliveryStatus)
	}
}
