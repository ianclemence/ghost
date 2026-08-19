package ghoststate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
)

// conversationWorkspace builds a database with the shape of real evidence:
// two sessions, mixed roles, tool metadata, an archived message, a session
// with zero messages, and a deliberate duplicate timestamp so ordering is
// proven by seq rather than by timestamp parsing.
func conversationWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec(`INSERT INTO sessions (id, summary, created_at, updated_at) VALUES ('sess-1', 'cooking preferences', '2026-01-01T00:00:00.000Z', '2026-01-02T00:00:00.000Z')`)
	exec(`INSERT INTO messages (id, session_id, role, content, meta, archived, created_at) VALUES ('m-1', 'sess-1', 'user', 'I love spaghetti', '{"tool_call_id":"t1"}', 0, '2026-01-01T00:00:01.000Z')`)
	exec(`INSERT INTO messages (id, session_id, role, content, meta, archived, created_at) VALUES ('m-2', 'sess-1', 'assistant', 'Let me remember that.', NULL, 0, '2026-01-01T00:00:02.000Z')`)
	exec(`INSERT INTO messages (id, session_id, role, content, meta, archived, created_at) VALUES ('m-3', 'sess-1', 'user', 'Add parmesan too', NULL, 0, '2026-01-01T00:00:02.000Z')`)
	exec(`INSERT INTO messages (id, session_id, role, content, meta, archived, created_at) VALUES ('m-4', 'sess-1', 'tool', '{"name":"remember"}', '{"tool_calls":[]}', 1, '2026-01-01T00:00:03.000Z')`)
	exec(`INSERT INTO sessions (id, summary) VALUES ('sess-empty', 'archived session with no messages')`)
	if err := d.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return ws
}

func exportAndRead(t *testing.T, ws string) (string, *Manifest, map[string][]byte) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	blob, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	plain, err := decryptBytes(blob, testPassphrase)
	if err != nil {
		t.Fatalf("decrypt archive: %v", err)
	}
	m, files, err := readArchive(plain)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	return archive, m, files
}

func TestConversationsExportDeterministic(t *testing.T) {
	ws := conversationWorkspace(t)

	_, m1, files1 := exportAndRead(t, ws)
	_, _, files2 := exportAndRead(t, ws)

	if m1.File(ghostDBLogical) != nil {
		t.Fatal("ghost.db must not be embedded in the archive")
	}
	reboundDB := false
	for _, r := range m1.Rebound {
		if strings.Contains(r, "ghost.db") {
			reboundDB = true
		}
	}
	if !reboundDB {
		t.Fatalf("ghost.db should be recorded as rebound: %v", m1.Rebound)
	}

	var convoFiles []string
	for name := range files1 {
		if strings.HasPrefix(name, conversationsDirLogical+"/") {
			convoFiles = append(convoFiles, name)
		}
	}
	if len(convoFiles) != 3 { // format.json + two session files
		t.Fatalf("expected format.json + 2 session files, got %v", convoFiles)
	}

	for _, name := range convoFiles {
		a, ok1 := files1[name]
		b, ok2 := files2[name]
		if !ok1 || !ok2 {
			t.Fatalf("conversations file %s present in only one export", name)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("conversations file %s is not deterministic between exports", name)
		}
	}
}

func TestConversationsRoundTripFreshDatabase(t *testing.T) {
	ws := conversationWorkspace(t)
	archive, m, files := exportAndRead(t, ws)
	if m.File(conversationsFormatLogical) == nil {
		t.Fatal("conversations format marker missing from archive")
	}
	if _, ok := files[conversationsFormatLogical]; !ok {
		t.Fatal("conversations format marker missing from archive payload")
	}

	targetWS := t.TempDir()
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	d, err := db.NewDB(targetWS)
	if err != nil {
		t.Fatalf("open target db: %v", err)
	}
	defer d.Close()

	var sessions int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 2 {
		t.Fatalf("sessions: got %d, want 2", sessions)
	}
	var summary string
	if err := d.QueryRow(`SELECT summary FROM sessions WHERE id = 'sess-1'`).Scan(&summary); err != nil {
		t.Fatalf("read sess-1 summary: %v", err)
	}
	if summary != "cooking preferences" {
		t.Fatalf("sess-1 summary = %q, want cooking preferences", summary)
	}
	var emptySummary string
	if err := d.QueryRow(`SELECT summary FROM sessions WHERE id = 'sess-empty'`).Scan(&emptySummary); err != nil {
		t.Fatalf("read sess-empty summary: %v", err)
	}
	if emptySummary != "archived session with no messages" {
		t.Fatalf("empty session summary = %q, want the zero-message session to survive", emptySummary)
	}

	var msgs int
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgs); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgs != 4 {
		t.Fatalf("messages: got %d, want 4", msgs)
	}

	// Order is an invariant of the format (seq), and the rehydrated database
	// must return messages in the same order the source did.
	rows, err := d.Query(`SELECT id FROM messages WHERE session_id = 'sess-1' ORDER BY created_at ASC, rowid ASC`)
	if err != nil {
		t.Fatalf("query order: %v", err)
	}
	var order []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		order = append(order, id)
	}
	rows.Close()
	want := []string{"m-1", "m-2", "m-3", "m-4"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}

	// Meta and timestamps are preserved byte-for-byte (SQLite normalizes the
	// timestamp on storage, so we assert against the stored form).
	var meta, createdAt string
	if err := d.QueryRow(`SELECT meta, created_at FROM messages WHERE id = 'm-1'`).Scan(&meta, &createdAt); err != nil {
		t.Fatalf("read m-1 meta: %v", err)
	}
	if meta != `{"tool_call_id":"t1"}` {
		t.Fatalf("m-1 meta = %q, want preserved", meta)
	}
	if createdAt != "2026-01-01T00:00:01Z" {
		t.Fatalf("m-1 created_at = %q, want preserved", createdAt)
	}
	var archived int
	if err := d.QueryRow(`SELECT archived FROM messages WHERE id = 'm-4'`).Scan(&archived); err != nil {
		t.Fatalf("read m-4 archived: %v", err)
	}
	if archived != 1 {
		t.Fatalf("m-4 archived = %d, want 1", archived)
	}

	// The FTS index is rebuilt by the insert triggers, so search works on the
	// fresh database without any extra step.
	var fts int
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'spaghetti'`).Scan(&fts); err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if fts != 1 {
		t.Fatalf("fts 'spaghetti' hits = %d, want 1", fts)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'parmesan'`).Scan(&fts); err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if fts != 1 {
		t.Fatalf("fts 'parmesan' hits = %d, want 1", fts)
	}
}
