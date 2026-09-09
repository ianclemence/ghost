package schema

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openRaw(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ghost.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func tablesOf(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[n] = true
	}
	return out
}

// Every table the export snapshots and the runtime queries must exist
// after migration: this is the contract MigrateToCurrent guarantees.
var requiredTables = []string{
	"schema_migrations",
	"sessions", "messages", "kv_store", "memory_chunks",
	"pending_pairings", "paired_devices", "jobs",
	"scheduled_items", "execution_history",
	"permission_grants", "permission_requests",
	"routine_meta",
	"canonical_events",
	"event_consumers", "event_claims",
	"computer_leases",
	"browser_sessions",
	"tool_usage",
}

func TestFreshDBReachesCurrent(t *testing.T) {
	db, _ := openRaw(t)
	v, err := MigrateToCurrent(db)
	if err != nil {
		t.Fatalf("MigrateToCurrent: %v", err)
	}
	if v != CurrentVersion {
		t.Fatalf("version = %d, want %d", v, CurrentVersion)
	}
	tables := tablesOf(t, db)
	for _, tbl := range requiredTables {
		if !tables[tbl] {
			t.Fatalf("table %s missing after migration", tbl)
		}
	}
	ok, at, err := CheckCurrent(db)
	if err != nil || !ok || at != CurrentVersion {
		t.Fatalf("CheckCurrent = %v, %d, %v; want true, %d", ok, at, err, CurrentVersion)
	}
	// Idempotent: second run is a no-op success.
	if v2, err := MigrateToCurrent(db); err != nil || v2 != CurrentVersion {
		t.Fatalf("second migrate: v=%d err=%v", v2, err)
	}
}

// An old database with only core tables and real user data migrates to
// current without losing a row. The sessions table deliberately lacks the
// title column (a real historical shape) to prove the baseline's
// backward-compatibility ALTER still applies.
func TestOldDBMigratesWithDataPreserved(t *testing.T) {
	db, _ := openRaw(t)
	for _, s := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, summary TEXT)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO sessions (id, summary) VALUES ('s1', 'user data must survive')`,
		`INSERT INTO messages (id, session_id, role, content) VALUES ('m1', 's1', 'user', 'hello')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed old schema: %v", err)
		}
	}
	v, err := MigrateToCurrent(db)
	if err != nil {
		t.Fatalf("MigrateToCurrent: %v", err)
	}
	if v != CurrentVersion {
		t.Fatalf("version = %d, want %d", v, CurrentVersion)
	}
	tables := tablesOf(t, db)
	for _, tbl := range requiredTables {
		if !tables[tbl] {
			t.Fatalf("table %s missing after migration", tbl)
		}
	}
	var summary, content string
	if err := db.QueryRow(`SELECT summary FROM sessions WHERE id='s1'`).Scan(&summary); err != nil || summary != "user data must survive" {
		t.Fatalf("session row lost: %q %v", summary, err)
	}
	var titleCol sql.NullString
	if err := db.QueryRow(`SELECT title FROM sessions WHERE id='s1'`).Scan(&titleCol); err != nil {
		t.Fatalf("title column not backfilled by baseline: %v", err)
	}
	if err := db.QueryRow(`SELECT content FROM messages WHERE id='m1'`).Scan(&content); err != nil || content != "hello" {
		t.Fatalf("message row lost: %q %v", content, err)
	}
}
