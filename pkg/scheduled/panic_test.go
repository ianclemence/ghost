package scheduled

import (
	"context"
	"testing"
	"time"
)

// A panicking executor must be contained: runItemSafely recovers so one bad
// scheduled run cannot crash the appliance.
func TestRunItemSafelyRecoversExecutorPanic(t *testing.T) {
	store := NewStore(openTestDB(t))
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	now := time.Now().UTC()
	item := &ScheduledItem{
		ID: "panic-1", Type: TypeReminder, Title: "boom",
		State: StateScheduled, NextRunAt: &now,
		Schedule: Schedule{Kind: ScheduleEvery, Every: time.Hour},
		Action:   Action{Kind: "message", Content: "x"},
	}
	if err := store.Create(item); err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(store, nil, func(_ context.Context, _ *ScheduledItem) error {
		panic("executor blew up")
	})
	// Must not panic out of the call.
	svc.runItemSafely(item)
}

// A nil store is also contained rather than crashing the process.
func TestRunItemSafelyRecoversNilStore(t *testing.T) {
	svc := NewService(nil, nil, nil)
	svc.runItemSafely(&ScheduledItem{ID: "x"})
}
