package maintenance

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestPruneOldNDJSON(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "2020-01-01.ndjson")
	if err := os.WriteFile(old, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, time.Now().Add(-400*24*time.Hour), time.Now().Add(-400*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh.ndjson")
	os.WriteFile(fresh, []byte("{}\n"), 0600)
	rep := Run(filepath.Dir(dir)+"/ws-never", nil)
	_ = rep
	// Run directly against dir via Report path: use parent workspace.
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "events"), 0755)
	os.WriteFile(filepath.Join(ws, "events", "2020-01-01.ndjson"), []byte("{}\n"), 0600)
	os.Chtimes(filepath.Join(ws, "events", "2020-01-01.ndjson"), time.Now().Add(-400*24*time.Hour), time.Now().Add(-400*24*time.Hour))
	rep2 := Run(ws, nil)
	found := false
	for _, a := range rep2.Actions {
		if a.Name == "event-logs" && a.Removed == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("old ndjson must be pruned: %+v", rep2.Actions)
	}
	if _, err := os.Stat(filepath.Join(ws, "events", "2020-01-01.ndjson")); !os.IsNotExist(err) {
		t.Fatal("old file must be gone")
	}
}

func TestCapHeartbeatLog(t *testing.T) {
	ws := t.TempDir()
	big := strings.Repeat("log line ... padding to grow the file quickly xxxxxxxxxx\n", 30000)
	os.WriteFile(filepath.Join(ws, "heartbeat.log"), []byte(big), 0644)
	rep := Run(ws, nil)
	found := false
	for _, a := range rep.Actions {
		if a.Name == "heartbeat-log" && a.Removed == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("oversize log must be capped: %+v", rep.Actions)
	}
	info, _ := os.Stat(filepath.Join(ws, "heartbeat.log"))
	if info.Size() > HeartbeatLogMax {
		t.Fatal("log still over cap")
	}
}

func TestPruneEventsTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.Exec(`CREATE TABLE canonical_events (seq INTEGER PRIMARY KEY, id TEXT, type TEXT, timestamp TEXT)`)
	old := time.Now().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	fresh := time.Now().Format(time.RFC3339)
	db.Exec(`INSERT INTO canonical_events (id, type, timestamp) VALUES ('o1','agent.progress',?)`, old)
	db.Exec(`INSERT INTO canonical_events (id, type, timestamp) VALUES ('o2','agent.progress',?)`, fresh)
	db.Exec(`INSERT INTO canonical_events (id, type, timestamp) VALUES ('o3','message.created',?)`, old)
	db.Exec(`INSERT INTO canonical_events (id, type, timestamp) VALUES ('o4','message.created',?)`, fresh)
	rep := Run(t.TempDir(), db)
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM canonical_events`).Scan(&n)
	if n != 2 {
		t.Fatalf("must keep fresh transient + fresh durable, got %d", n)
	}
	_ = rep
}

func TestRunNeverFailsClosed(t *testing.T) {
	// Missing workspace/db must report, never panic or error out.
	rep := Run(t.TempDir()+"/nonexistent", nil)
	if rep.At == "" || len(rep.Actions) == 0 {
		t.Fatal("run must always report")
	}
}
