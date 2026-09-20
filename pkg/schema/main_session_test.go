package schema

import (
	"testing"
)

// Upgrading to v6 folds the pre-unification home sessions into main:
// messages interleave by time, the freshest custom title survives, legacy
// ledger rows disappear, and unrelated sessions are untouched. Re-running
// is a no-op.
func TestV6MainSessionFold(t *testing.T) {
	db, _ := openRaw(t)

	if _, err := MigrateToCurrent(db); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	seeds := []string{
		`INSERT INTO sessions (id, title, created_at, updated_at) VALUES ('mobile:default', 'Phone chat', '2026-01-01', '2026-01-02')`,
		`INSERT INTO sessions (id, title, created_at, updated_at) VALUES ('cli:default', 'Terminal chat', '2026-01-01', '2026-01-03')`,
		`INSERT INTO sessions (id, title, created_at, updated_at) VALUES ('voice:keep', 'Voice note', '2026-01-01', '2026-01-01')`,
		`INSERT INTO messages (id, session_id, role, content, created_at) VALUES ('m1', 'mobile:default', 'user', 'from app', '2026-01-01T10:00:00Z')`,
		`INSERT INTO messages (id, session_id, role, content, created_at) VALUES ('m2', 'cli:default', 'user', 'from terminal', '2026-01-01T11:00:00Z')`,
		`INSERT INTO messages (id, session_id, role, content, created_at) VALUES ('m3', 'voice:keep', 'user', 'voice', '2026-01-01T12:00:00Z')`,
	}
	for _, s := range seeds {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
	// Pretend this device predates v6 so only v6 runs.
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version >= 6`); err != nil {
		t.Fatalf("rewind version: %v", err)
	}
	if _, err := MigrateToCurrent(db); err != nil {
		t.Fatalf("migrate v6: %v", err)
	}

	var mainCount, legacyCount, voiceCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 'main'`).Scan(&mainCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id IN ('mobile:default','cli:default')`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 'voice:keep'`).Scan(&voiceCount); err != nil {
		t.Fatal(err)
	}
	if mainCount != 2 || legacyCount != 0 || voiceCount != 1 {
		t.Fatalf("fold wrong: main=%d legacy=%d voice=%d", mainCount, legacyCount, voiceCount)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM sessions WHERE id = 'main'`).Scan(&title); err != nil {
		t.Fatalf("main title missing: %v", err)
	}
	if title != "Terminal chat" {
		t.Errorf("freshest title should survive, got %q", title)
	}
	var ledgers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id IN ('mobile:default','cli:default')`).Scan(&ledgers); err != nil {
		t.Fatal(err)
	}
	if ledgers != 0 {
		t.Errorf("legacy ledger rows must go, found %d", ledgers)
	}

	// Idempotent: run the head migration again, nothing changes.
	if _, err := MigrateToCurrent(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 'main'`).Scan(&mainCount); err != nil || mainCount != 2 {
		t.Fatalf("re-run must be a no-op, main=%d err=%v", mainCount, err)
	}
}
