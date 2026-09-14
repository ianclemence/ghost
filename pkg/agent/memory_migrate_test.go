package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Legacy month-nested notes migrate to the flat Muse layout exactly once;
// existing flat files win and nothing is lost.
func TestMigrateDayNotesToFlatLayout(t *testing.T) {
	ws := t.TempDir()
	memDir := filepath.Join(ws, "memory")
	legacyDir := filepath.Join(memDir, "202609")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "20260903.md"), []byte("# 2026-09-03\nlegacy note\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "2026-09-04.md"), []byte("# 2026-09-04\nflat note\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ms := NewMemoryStore(ws)
	data, err := os.ReadFile(filepath.Join(memDir, "2026-09-03.md"))
	if err != nil {
		t.Fatalf("legacy note must migrate to flat layout: %v", err)
	}
	if string(data) != "# 2026-09-03\nlegacy note\n" {
		t.Fatalf("migrated content must be intact, got %q", data)
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "20260903.md")); !os.IsNotExist(err) {
		t.Fatal("migrated legacy file must be removed")
	}
	if data, err := os.ReadFile(filepath.Join(memDir, "2026-09-04.md")); err != nil || string(data) != "# 2026-09-04\nflat note\n" {
		t.Fatal("pre-existing flat file must be untouched")
	}
	if got := ms.dailyFile(time.Now()); got == "" {
		t.Fatal("dailyFile must return a path")
	}
}

// DigestLongTerm rebuilds MEMORY.md from structured memory; the file is a
// generated digest, never a second truth.
func TestDigestLongTermRegenerates(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	if err := os.MkdirAll(filepath.Join(ws, "personal-context"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ms.DigestLongTerm(); err != nil {
		t.Fatalf("digest of empty store must succeed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("digest must write MEMORY.md")
	}
}
