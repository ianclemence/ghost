package tools

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCuratorEnsureSchema(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	if err := c.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tool_usage'`).Scan(&count)
	if count != 1 {
		t.Fatal("tool_usage table not created")
	}
}

func TestCuratorRecordUsage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("bash")
	c.RecordUsage("bash")
	c.RecordUsage("read_file")
	records := c.GetRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	r, ok := c.GetState("bash")
	if !ok || r != ToolStateActive {
		t.Fatalf("expected bash active, got %v", r)
	}
}

func TestCuratorRecordUsageDisabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: false}
	c := NewCurator(db, cfg)
	c.RecordUsage("bash")
	records := c.GetRecords()
	if len(records) != 0 {
		t.Fatalf("expected 0 records when disabled, got %d", len(records))
	}
}

func TestCuratorStaleTransition(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("old_tool")
	c.mu.Lock()
	r := c.records["old_tool"]
	r.LastUsedAt = time.Now().AddDate(0, 0, -31)
	c.db.Exec(`UPDATE tool_usage SET last_used_at = ? WHERE name = ?`, r.LastUsedAt, "old_tool")
	c.mu.Unlock()
	transitions := c.RunCheck()
	if state, ok := transitions["old_tool"]; !ok || state != ToolStateStale {
		t.Fatalf("expected old_tool to go stale, got %v", state)
	}
}

func TestCuratorArchiveTransition(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("ancient_tool")
	c.mu.Lock()
	r := c.records["ancient_tool"]
	r.LastUsedAt = time.Now().AddDate(0, 0, -91)
	c.db.Exec(`UPDATE tool_usage SET last_used_at = ? WHERE name = ?`, r.LastUsedAt, "ancient_tool")
	c.mu.Unlock()
	transitions := c.RunCheck()
	if state, ok := transitions["ancient_tool"]; !ok || state != ToolStateArchived {
		t.Fatalf("expected ancient_tool to archive, got %v", state)
	}
}

func TestCuratorPinPreventsArchive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("pinned_tool")
	c.PinTool("pinned_tool")
	c.mu.Lock()
	r := c.records["pinned_tool"]
	r.LastUsedAt = time.Now().AddDate(0, 0, -91)
	c.db.Exec(`UPDATE tool_usage SET last_used_at = ? WHERE name = ?`, r.LastUsedAt, "pinned_tool")
	c.mu.Unlock()
	transitions := c.RunCheck()
	if _, ok := transitions["pinned_tool"]; ok {
		t.Fatal("pinned tool should not transition")
	}
}

func TestCuratorReactivation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("reactivating_tool")
	c.mu.Lock()
	r := c.records["reactivating_tool"]
	r.LastUsedAt = time.Now().AddDate(0, 0, -31)
	r.State = ToolStateStale
	c.db.Exec(`UPDATE tool_usage SET last_used_at = ?, state = 'stale' WHERE name = ?`, r.LastUsedAt, "reactivating_tool")
	c.mu.Unlock()
	// RecordUsage should reactivate the tool
	c.RecordUsage("reactivating_tool")
	state, _ := c.GetState("reactivating_tool")
	if state != ToolStateActive {
		t.Fatalf("expected reactivating_tool to be active after usage, got %v", state)
	}
}

func TestCuratorGracePeriod(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("new_tool")
	transitions := c.RunCheck()
	if _, ok := transitions["new_tool"]; ok {
		t.Fatal("new tool should not transition during grace period")
	}
}

func TestCuratorPinUnpin(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("tool_a")
	if err := c.PinTool("tool_a"); err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	pinned := c.records["tool_a"].Pinned
	c.mu.RUnlock()
	if !pinned {
		t.Fatal("expected tool to be pinned")
	}
	if err := c.UnpinTool("tool_a"); err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	pinned = c.records["tool_a"].Pinned
	c.mu.RUnlock()
	if pinned {
		t.Fatal("expected tool to be unpinned")
	}
}

func TestCuratorPinNonexistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	if err := c.PinTool("nonexistent"); err == nil {
		t.Fatal("expected error pinning nonexistent tool")
	}
}

func TestCuratorStartStop(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90, CheckIntervalMins: 1}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	ctx, cancel := context.WithCancel(context.Background())
	go c.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
}

func TestCuratorGetRecordsEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	records := c.GetRecords()
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestCuratorMultipleRuns(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	c.RecordUsage("tool_a")
	c.RecordUsage("tool_b")
	c.RecordUsage("tool_c")
	trans1 := c.RunCheck()
	if len(trans1) != 0 {
		t.Fatalf("expected no transitions on first run, got %d", len(trans1))
	}
	trans2 := c.RunCheck()
	if len(trans2) != 0 {
		t.Fatalf("expected no transitions on second run, got %d", len(trans2))
	}
}

func TestCuratorNeverUsedArchive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	cfg := CuratorConfig{Enabled: true, StaleAfterDays: 30, ArchiveAfterDays: 90}
	c := NewCurator(db, cfg)
	c.EnsureSchema()
	now := time.Now()
	c.mu.Lock()
	c.records["unused_tool"] = &ToolRecord{
		Name:       "unused_tool",
		State:      ToolStateActive,
		UseCount:   0,
		LastUsedAt: now.AddDate(0, 0, -100),
		CreatedAt:  now.AddDate(0, 0, -100),
	}
	c.db.Exec(`INSERT INTO tool_usage (name, state, use_count, last_used_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		"unused_tool", "active", 0, now.AddDate(0, 0, -100), now.AddDate(0, 0, -100))
	c.mu.Unlock()
	transitions := c.RunCheck()
	if state, ok := transitions["unused_tool"]; !ok || state != ToolStateArchived {
		t.Fatalf("expected unused tool past archive threshold to archive, got %v", state)
	}
}
