package scheduled

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMigrateFromCronJSON(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	cronJSONPath := filepath.Join(tmpDir, "jobs.json")

	// Create test data in old format
	createdAt := time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2025, 8, 31, 12, 0, 0, 0, time.UTC)
	lastRun := time.Date(2025, 8, 31, 8, 0, 0, 0, time.UTC)
	nextRun := time.Date(2025, 9, 7, 8, 0, 0, 0, time.UTC)
	everyMS := int64(86400000) // 24 hours

	oldStore := CronStore{
		Version: 1,
		Jobs: []CronJob{
			{
				ID:             "test-job-1",
				Name:           "Weekly briefing",
				Enabled:        true,
				LifecycleState: "active",
				RunCount:       5,
				LastRunAt:      &lastRun,
				NextRunAt:      &nextRun,
				Schedule: CronSchedule{
					Kind:    "cron",
					Expr:    "0 8 * * 1",
					Timezone: "Asia/Bangkok",
				},
				Payload: CronPayload{
					Kind:    "agent_turn",
					Message: "Prepare my weekly briefing",
					Deliver: true,
					Channel: "telegram",
				},
				State: CronJobState{
					NextRunAtMS: int64Ptr(nextRun.UnixMilli()),
					LastRunAtMS: int64Ptr(lastRun.UnixMilli()),
					LastStatus:  "ok",
				},
				CreatedAtMS: createdAt.UnixMilli(),
				UpdatedAtMS: updatedAt.UnixMilli(),
				Skills:      []string{"weekly-briefing"},
			},
			{
				ID:             "test-job-2",
				Name:           "Daily reminder",
				Enabled:        true,
				LifecycleState: "active",
				Schedule: CronSchedule{
					Kind:    "every",
					EveryMS: &everyMS,
				},
				Payload: CronPayload{
					Kind:    "agent_turn",
					Message: "Time for your daily check-in",
				},
				CreatedAtMS: createdAt.UnixMilli(),
				UpdatedAtMS: updatedAt.UnixMilli(),
			},
		},
	}

	data, _ := json.Marshal(oldStore)
	os.WriteFile(cronJSONPath, data, 0644)

	// Create store
	store := NewStore(openTestDB(t))
	defer store.db.Close()
	if err := store.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	// Run migration
	migrated, err := MigrateFromCronJSON(store, cronJSONPath)
	if err != nil {
		t.Fatalf("MigrateFromCronJSON failed: %v", err)
	}

	if migrated != 2 {
		t.Errorf("expected 2 migrated items, got %d", migrated)
	}

	// Verify first item
	item1, err := store.Get("test-job-1")
	if err != nil {
		t.Fatalf("failed to get item 1: %v", err)
	}

	if item1.Title != "Weekly briefing" {
		t.Errorf("expected title 'Weekly briefing', got %q", item1.Title)
	}
	if item1.Type != TypeAutomation {
		t.Errorf("expected type automation, got %q", item1.Type)
	}
	if item1.State != StateScheduled {
		t.Errorf("expected state scheduled, got %q", item1.State)
	}
	if item1.Schedule.Kind != ScheduleCron {
		t.Errorf("expected schedule kind cron, got %q", item1.Schedule.Kind)
	}
	if item1.Schedule.Expr != "0 8 * * 1" {
		t.Errorf("expected schedule expr '0 8 * * 1', got %q", item1.Schedule.Expr)
	}
	if item1.Action.Content != "Prepare my weekly briefing" {
		t.Errorf("expected action content 'Prepare my weekly briefing', got %q", item1.Action.Content)
	}
	if item1.Channel != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", item1.Channel)
	}
	if item1.RunCount != 5 {
		t.Errorf("expected run count 5, got %d", item1.RunCount)
	}

	// Verify second item
	item2, err := store.Get("test-job-2")
	if err != nil {
		t.Fatalf("failed to get item 2: %v", err)
	}

	if item2.Title != "Daily reminder" {
		t.Errorf("expected title 'Daily reminder', got %q", item2.Title)
	}
	if item2.Schedule.Kind != ScheduleEvery {
		t.Errorf("expected schedule kind every, got %q", item2.Schedule.Kind)
	}
	if item2.Schedule.Every != 24*time.Hour {
		t.Errorf("expected schedule every 24h, got %v", item2.Schedule.Every)
	}
}

func TestMigrateFromCronJSON_FileNotFound(t *testing.T) {
	store := NewStore(openTestDB(t))
	defer store.db.Close()
	if err := store.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	migrated, err := MigrateFromCronJSON(store, "/nonexistent/jobs.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if migrated != 0 {
		t.Errorf("expected 0 migrated items, got %d", migrated)
	}
}

func TestMigrateFromCronJSON_AlreadyMigrated(t *testing.T) {
	tmpDir := t.TempDir()
	cronJSONPath := filepath.Join(tmpDir, "jobs.json")

	// Create test data
	oldStore := CronStore{
		Version: 1,
		Jobs: []CronJob{
			{
				ID:      "existing-job",
				Name:    "Existing",
				Enabled: true,
				Schedule: CronSchedule{
					Kind: "cron",
					Expr: "0 8 * * *",
				},
				Payload: CronPayload{
					Kind:    "agent_turn",
					Message: "Test",
				},
				CreatedAtMS: time.Now().UnixMilli(),
				UpdatedAtMS: time.Now().UnixMilli(),
			},
		},
	}

	data, _ := json.Marshal(oldStore)
	os.WriteFile(cronJSONPath, data, 0644)

	// Create store and pre-populate
	store := NewStore(openTestDB(t))
	defer store.db.Close()
	if err := store.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	existing := &ScheduledItem{
		ID:    "existing-job",
		Type:  TypeAutomation,
		Title: "Already migrated",
		State: StateScheduled,
		Schedule: Schedule{
			Kind: ScheduleCron,
			Expr: "0 8 * * *",
		},
		Action: Action{
			Kind:    ActionAgentTurn,
			Content: "Already migrated",
		},
	}
	store.Create(existing)

	// Run migration - should skip existing
	migrated, err := MigrateFromCronJSON(store, cronJSONPath)
	if err != nil {
		t.Fatalf("MigrateFromCronJSON failed: %v", err)
	}

	if migrated != 0 {
		t.Errorf("expected 0 migrated items (already exists), got %d", migrated)
	}

	// Verify existing item unchanged
	item, err := store.Get("existing-job")
	if err != nil {
		t.Fatalf("failed to get item: %v", err)
	}
	if item.Title != "Already migrated" {
		t.Errorf("expected title unchanged, got %q", item.Title)
	}
}

func TestConvertState(t *testing.T) {
	cases := []struct {
		oldState string
		enabled  bool
		want     ItemState
	}{
		{"active", true, StateScheduled},
		{"active", false, StatePaused},
		{"paused", true, StatePaused},
		{"paused", false, StatePaused},
		{"running", true, StateRunning},
		{"", true, StateScheduled},
		{"", false, StatePaused},
	}
	for _, tc := range cases {
		got := convertState(tc.oldState, tc.enabled)
		if got != tc.want {
			t.Errorf("convertState(%q, %v) = %q, want %q", tc.oldState, tc.enabled, got, tc.want)
		}
	}
}

func TestConvertSchedule(t *testing.T) {
	atMS := int64(1725676800000) // 2024-09-07 08:00:00 UTC
	everyMS := int64(3600000)    // 1 hour

	cases := []struct {
		old      CronSchedule
		wantKind ScheduleKind
	}{
		{CronSchedule{Kind: "at", AtMS: &atMS}, ScheduleAt},
		{CronSchedule{Kind: "every", EveryMS: &everyMS}, ScheduleEvery},
		{CronSchedule{Kind: "cron", Expr: "0 8 * * *"}, ScheduleCron},
	}
	for _, tc := range cases {
		got := convertSchedule(tc.old)
		if got.Kind != tc.wantKind {
			t.Errorf("convertSchedule kind = %q, want %q", got.Kind, tc.wantKind)
		}
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := conn.Ping(); err != nil {
		t.Fatalf("failed to ping test db: %v", err)
	}
	return conn
}
