package scheduled

import (
	"testing"
	"time"
)

func TestNextCronRun(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) // Wed 10:00 UTC

	tests := []struct {
		name string
		expr string
		tz   string
		want string
	}{
		{"daily 9am", "0 9 * * *", "UTC", "2026-09-03 09:00:00 +0000 UTC"},
		{"monday 9am", "0 9 * * 1", "UTC", "2026-09-07 09:00:00 +0000 UTC"},
		{"month 1st 9am", "0 9 1 * *", "UTC", "2026-10-01 09:00:00 +0000 UTC"},
		{"monday midnight", "0 0 * * 1", "UTC", "2026-09-07 00:00:00 +0000 UTC"},
		{"past today already fired", "0 8 * * *", "UTC", "2026-09-03 08:00:00 +0000 UTC"},
		{"active timezone converted", "0 9 * * *", "Asia/Bangkok", "2026-09-03 02:00:00 +0000 UTC"},
		{"invalid expr returns nil", "not a cron", "UTC", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextCronRun(tt.expr, tt.tz, now)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("expected nil for invalid expr, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("unexpected nil next run for %q", tt.expr)
			}
			if got.UTC().String() != tt.want {
				t.Errorf("NextCronRun(%q) = %v, want %v", tt.expr, got.UTC(), tt.want)
			}
		})
	}
}

// A routine created without a next run was never selected by ListDue: it sat
// "active" and never fired. Every waiting item must carry one.
func TestFirstRunAndRepair(t *testing.T) {
	store := newMigrateStore(t)
	svc := NewService(store, &SimpleEventBus{}, nil)
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC) // 08:00 Bangkok
	item := &ScheduledItem{ID: "r1", Type: TypeAutomation, Title: "Weather", State: StateScheduled,
		Schedule: Schedule{Kind: ScheduleCron, Expr: "30 7 * * *"}, Timezone: "Asia/Bangkok",
		Action: Action{Kind: ActionAgentTurn, Content: "weather"}, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(item); err != nil {
		t.Fatal(err)
	}
	if n := svc.RepairMissingNextRun(now); n != 1 {
		t.Fatalf("repaired %d, want 1", n)
	}
	got, _ := store.Get("r1")
	want := time.Date(2026, 10, 4, 0, 30, 0, 0, time.UTC) // tomorrow 07:30 Bangkok
	if got.NextRunAt == nil || !got.NextRunAt.Equal(want) {
		t.Fatalf("next run %v, want %v", got.NextRunAt, want)
	}
	if n := svc.RepairMissingNextRun(now); n != 0 {
		t.Fatalf("repair is not idempotent: %d", n)
	}
}
