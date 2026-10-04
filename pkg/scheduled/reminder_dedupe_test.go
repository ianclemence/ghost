package scheduled

import (
	"testing"
	"time"
)

// A reminder that fired twice is still one reminder. A retry, a restart, or
// two ticks racing a slow boot all record a second delivery, but the owner
// made one promise to themselves — so OpenDeliveries returns one row, and
// every consumer (the morning note, "still open from…") counts one.
func TestReminderDeliveredTwiceCountsOnce(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	newReminder(t, store, "r1", now)
	_ = store.UpdateState("r1", StateCompleted)

	if err := svc.RecordDelivery("r1", now); err != nil {
		t.Fatal(err)
	}
	second := now.Add(7 * time.Second)
	if err := svc.RecordDelivery("r1", second); err != nil {
		t.Fatal(err)
	}

	open, err := svc.OpenDeliveries(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("open = %d rows for one reminder, want 1", len(open))
	}
	if !open[0].DeliveredAt.Equal(second) {
		t.Fatalf("open delivery = %v, want the most recent fire", open[0].DeliveredAt)
	}
}

// Answering a reminder settles every delivery it produced, so none of them
// stays open to be raised again as a second thing.
func TestAnsweringReminderSettlesEveryDelivery(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	newReminder(t, store, "r1", now)
	_ = store.UpdateState("r1", StateCompleted)

	_ = svc.RecordDelivery("r1", now)
	_ = svc.RecordDelivery("r1", now.Add(7*time.Second))

	if _, err := svc.MarkDone("r1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := openAcks(t, store, "r1"); n != 0 {
		t.Fatalf("%d deliveries of a done reminder are still open", n)
	}
}

// Seeing a reminder settles every unseen delivery too: marking one of the
// two rows seen left the item looking unseen.
func TestSeenSettlesEveryDelivery(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	newReminder(t, store, "r1", now)
	_ = store.UpdateState("r1", StateCompleted)

	_ = svc.RecordDelivery("r1", now)
	_ = svc.RecordDelivery("r1", now.Add(7*time.Second))

	if err := svc.MarkSeen("r1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if open, err := svc.OpenDeliveries(now.Add(-time.Hour), now.Add(time.Hour)); err != nil || len(open) != 0 {
		t.Fatalf("a seen reminder still reports open: %+v, %v", open, err)
	}
}

// openAcks counts an item's reminder_acks rows that no outcome has settled.
func openAcks(t *testing.T, store *Store, itemID string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM reminder_acks WHERE item_id=? AND COALESCE(outcome,'')=''`, itemID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
