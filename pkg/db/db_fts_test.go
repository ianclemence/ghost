package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFTS5CreatesAndIndexesMessages(t *testing.T) {
	workspace := t.TempDir()
	database, err := NewDB(workspace)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	_, err = database.Exec(`
		INSERT INTO messages (id, session_id, role, content)
		VALUES ('m1', 's1', 'user', 'hello fts world')
	`)
	if err != nil {
		t.Fatalf("insert message failed: %v", err)
	}

	var count int
	err = database.QueryRow(`
		SELECT COUNT(*)
		FROM messages_fts
		WHERE messages_fts MATCH 'fts'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("fts query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 FTS match, got %d", count)
	}

	if _, err := os.Stat(filepath.Join(workspace, "ghost.db")); err != nil {
		t.Fatalf("database file missing: %v", err)
	}
}
