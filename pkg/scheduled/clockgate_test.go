package scheduled

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
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

// A recurring automation that came due while Ghost was off is skipped, not
// replayed late, and the owner is told; a recurring reminder is a commitment
// and still goes out; a slightly late run is just a slow boot and still runs.
func TestTickAppliesMissedPolicyToRecurringItems(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "sched.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatal(err)
	}
	mk := func(title string, typ ItemType, late time.Duration) *ScheduledItem {
		due := time.Now().UTC().Add(-late)
		it := &ScheduledItem{
			Type: typ, Title: title, State: StateScheduled,
			Schedule: Schedule{Kind: ScheduleEvery, Every: 24 * time.Hour},
			Action:   Action{Kind: ActionMessage, Content: title}, Source: "test",
		}
		it.NextRunAt = &due
		if err := store.Create(it); err != nil {
			t.Fatal(err)
		}
		return it
	}
	stale := mk("morning brief", TypeAutomation, 13*time.Hour)
	reminder := mk("take pills", TypeReminder, 13*time.Hour)
	slow := mk("slow boot brief", TypeAutomation, 5*time.Minute)

	var ran sync.Map
	var told []string
	svc := NewService(store, &SimpleEventBus{}, func(ctx context.Context, it *ScheduledItem) error {
		ran.Store(it.Title, true)
		return nil
	})
	svc.MissedNotice = func(it *ScheduledItem, due time.Time) { told = append(told, it.Title) }
	svc.tick()
	time.Sleep(300 * time.Millisecond)

	if _, ok := ran.Load("morning brief"); ok {
		t.Fatal("a stale recurring automation must not be replayed")
	}
	if len(told) != 1 || told[0] != "morning brief" {
		t.Fatalf("the owner must be told about the skipped run, got %v", told)
	}
	got, _ := store.Get(stale.ID)
	if got.NextRunAt == nil || !got.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("the skipped item must move on to its next occurrence, got %v", got.NextRunAt)
	}
	if _, ok := ran.Load("take pills"); !ok {
		t.Fatal("a recurring reminder is a commitment and must still be delivered")
	}
	if _, ok := ran.Load("slow boot brief"); !ok {
		t.Fatal("a run that is only minutes late is a slow boot and must still run")
	}
	_, _ = reminder, slow
}
