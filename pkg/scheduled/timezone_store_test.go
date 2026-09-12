package scheduled

import (
	"strings"
	"testing"
	"time"
)

// Schedule instants must persist UTC: non-UTC TEXT breaks the
// lexicographic next_run_at <= now comparison in ListDue.
func TestCreateNormalizesTimesToUTC(t *testing.T) {
	store := NewStore(openTestDB(t))
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	at := time.Date(2026, 9, 12, 19, 45, 0, 0, loc)
	item := &ScheduledItem{
		Type:      TypeReminder,
		State:     StateScheduled,
		Timezone:  "Asia/Bangkok",
		Channel:   "mobile",
		ChatID:    "default",
		Schedule:  Schedule{Kind: ScheduleAt, At: &at},
		Action:    Action{Kind: ActionAgentTurn, Content: "x"},
		NextRunAt: &at,
	}
	if err := store.Create(item); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var raw string
	if err := store.db.QueryRow(`SELECT next_run_at FROM scheduled_items WHERE id = ?`, item.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if !containsUTC(raw) {
		t.Fatalf("stored next_run_at = %q, want UTC shape", raw)
	}
	got, err := store.Get(item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.NextRunAt.Equal(at) {
		t.Fatalf("round-trip instant changed: %v vs %v", got.NextRunAt, at)
	}
}

func containsUTC(s string) bool {
	return strings.Contains(s, "+0000") || strings.HasSuffix(s, "Z")
}

func mustBangkok(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

// Legacy offset-shaped rows (incl. monotonic suffixes) migrate to UTC,
// and a past-due migrated row becomes listable by ListDue.
func TestNormalizeStoredTimesToUTCRepairsLegacyRows(t *testing.T) {
	store := NewStore(openTestDB(t))
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	past := time.Now().UTC().Add(-2 * time.Minute)
	legacyAt := past.In(loc).String() // "...+0700 +07 m=+..."
	if _, err := store.db.Exec(`INSERT INTO scheduled_items
		(id, type, title, state, schedule_kind, schedule_at, timezone, action_kind, next_run_at, run_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-1", TypeReminder, "Legacy", StateScheduled, ScheduleAt,
		legacyAt, "Asia/Bangkok", ActionAgentTurn, legacyAt, 0,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	n, err := store.NormalizeStoredTimesToUTC()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if n == 0 {
		t.Fatal("expected legacy columns to be rewritten")
	}
	due, err := store.ListDue(time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	found := false
	for _, it := range due {
		if it.ID == "legacy-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("migrated past-due row not returned by ListDue")
	}
	// Second run is a no-op.
	n, err = store.NormalizeStoredTimesToUTC()
	if err != nil || n != 0 {
		t.Fatalf("second normalize = %d, %v; want 0, nil", n, err)
	}
}
