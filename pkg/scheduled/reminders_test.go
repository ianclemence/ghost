package scheduled

import (
	"testing"
	"time"
)

func newReminder(t *testing.T, store *Store, id string, at time.Time) {
	t.Helper()
	a := at.UTC()
	if err := store.Create(&ScheduledItem{ID: id, Type: TypeReminder, Title: "Water the plants", State: StateScheduled,
		Schedule: Schedule{Kind: ScheduleAt, At: &a}, Timezone: "Asia/Bangkok", NextRunAt: &a,
		Action: Action{Kind: ActionAgentTurn, Content: "Water the plants"}, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

// A reminder that went off keeps a record of what the owner did with it.
func TestReminderLifecycle(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	newReminder(t, store, "r1", now)

	// It fires: delivered, unseen, open.
	_ = store.UpdateState("r1", StateCompleted)
	if err := svc.RecordDelivery("r1", now); err != nil {
		t.Fatal(err)
	}
	open, err := svc.OpenDeliveries(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil || len(open) != 1 || open[0].Title != "Water the plants" {
		t.Fatalf("open = %+v, %v", open, err)
	}
	// Snooze keeps the same reminder and brings it back.
	later := now.Add(10 * time.Minute)
	item, err := svc.Snooze("r1", later, now)
	if err != nil || item.State != StateScheduled || !item.NextRunAt.Equal(later) {
		t.Fatalf("snooze = %+v, %v", item, err)
	}
	if open, _ := svc.OpenDeliveries(now.Add(-time.Hour), now.Add(time.Hour)); len(open) != 0 {
		t.Fatal("a snoozed delivery is not open")
	}
	// It fires again, and the owner says done.
	_ = store.UpdateState("r1", StateCompleted)
	_ = svc.RecordDelivery("r1", later)
	if item, err = svc.MarkDone("r1", later.Add(time.Minute)); err != nil || item.State != StateDone {
		t.Fatalf("done = %+v, %v", item, err)
	}
	if _, err := svc.Snooze("r1", later.Add(time.Hour), later); err != ErrClosed {
		t.Fatalf("acting on a closed reminder: %v", err)
	}
}

// "I already watered them": a reminder done before it fires never fires.
func TestDoneBeforeItFires(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	newReminder(t, store, "r2", now.Add(6*time.Hour))
	if _, err := svc.MarkDone("r2", now); err != nil {
		t.Fatal(err)
	}
	due, _ := store.ListDue(now.Add(7 * time.Hour))
	for _, it := range due {
		if it.ID == "r2" {
			t.Fatal("a reminder marked done still fires")
		}
	}
}

func TestSnoozeUntil(t *testing.T) {
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC) // 21:00 Bangkok
	got, err := SnoozeUntil("tomorrow", now, "Asia/Bangkok")
	if err != nil || !got.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("tomorrow = %v, %v", got, err)
	}
	if _, err := SnoozeUntil("forever", now, ""); err == nil {
		t.Fatal("nonsense snooze accepted")
	}
}
