package scheduled

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A blocked gate must hold due items without firing or losing them; an
// open gate fires exactly once. Uses a file DB so pooled connections see
// the same data.
func TestTickRespectsClockGate(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	item := &ScheduledItem{
		Type:     TypeReminder,
		Title:    "gated",
		State:    StateScheduled,
		Schedule: Schedule{Kind: ScheduleAt, At: &past},
		Action:   Action{Kind: ActionMessage, Content: "hi"},
		Source:   "test",
	}
	item.NextRunAt = &past
	if err := store.Create(item); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var calls atomic.Int32
	svc := NewService(store, &SimpleEventBus{}, func(ctx context.Context, it *ScheduledItem) error {
		calls.Add(1)
		return nil
	})

	svc.ClockGate = func() bool { return false }
	svc.tick()
	// executeItem runs async; give a blocked tick every chance to misfire.
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("blocked gate must not fire due items")
	}
	got, err := store.Get(item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != StateScheduled {
		t.Fatalf("blocked item must stay scheduled, got %v", got.State)
	}

	svc.ClockGate = func() bool { return true }
	svc.tick()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("open gate must fire exactly once, got %d", calls.Load())
	}
}
