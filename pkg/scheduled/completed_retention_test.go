package scheduled

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Fired one-shots must stay on the record as completed — that is what
// lets "check my reminders" show what already fired — while ListDue must
// never be able to pick them up again. Regression: one-time items were
// deleted after success, so fired reminders became unlistable history.
func TestOneTimeSuccessRetainedAsCompleted(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	svc, store := newLedgerTestService(t, func(context.Context, *ScheduledItem) error { return nil })
	item := ledgerTestItem(past)
	if err := svc.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	svc.executeItem(item)

	got, err := svc.GetItem(item.ID)
	if err != nil || got == nil {
		t.Fatalf("fired one-shot must remain on the schedule (err=%v)", err)
	}
	if got.State != StateCompleted {
		t.Fatalf("state = %q, want completed", got.State)
	}

	due, err := store.ListDue(time.Now().UTC().Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	for _, d := range due {
		if d.ID == item.ID {
			t.Fatalf("a completed item must never come due again")
		}
	}
}

// delete_after_run still means exactly what it says.
func TestDeleteAfterRunStillDeletes(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	svc, _ := newLedgerTestService(t, func(context.Context, *ScheduledItem) error { return nil })
	item := ledgerTestItem(past)
	item.DeleteAfterRun = true
	if err := svc.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	svc.executeItem(item)

	if got, err := svc.GetItem(item.ID); err == nil && got != nil {
		t.Fatalf("delete_after_run item must be gone, got state %q", got.State)
	}
}

// Recurring items keep their old behavior: the next run is computed and
// the row stays scheduled.
func TestRecurringSuccessStaysScheduled(t *testing.T) {
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
	if err != nil || got == nil {
		t.Fatalf("recurring item must survive its run (err=%v)", err)
	}
	if got.State != StateScheduled {
		t.Fatalf("state = %q, want scheduled", got.State)
	}
	if got.NextRunAt == nil {
		t.Fatal("next_run_at must be recomputed")
	}
}

// Retained history stays bounded: only the newest `keep` completed rows
// survive a prune.
func TestPruneCompletedBoundsHistory(t *testing.T) {
	store := NewStore(openTestDB(t))
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		it := &ScheduledItem{
			ID:        fmt.Sprintf("c%d", i),
			Type:      TypeReminder,
			Title:     fmt.Sprintf("done %d", i),
			State:     StateCompleted,
			Timezone:  "UTC",
			Schedule:  Schedule{Kind: ScheduleAt, At: &at},
			Action:    Action{Kind: ActionAgentTurn, Content: "x"},
			UpdatedAt: at,
		}
		if err := store.Create(it); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	n, err := store.PruneCompleted(2)
	if err != nil {
		t.Fatalf("PruneCompleted: %v", err)
	}
	if n != 3 {
		t.Fatalf("pruned = %d, want 3", n)
	}
	left, err := store.List("", StateCompleted, 50)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("remaining completed = %d, want 2", len(left))
	}
}
