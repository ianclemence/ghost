package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/schema"
)

// The intelligence check reports durable memory, task activity, recovery,
// verification failures, and observed retrieval latency — all from real
// state, never fabricated.
func TestCheckIntelligenceReportsState(t *testing.T) {
	ws := t.TempDir()
	database, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer database.Close()
	if _, err := schema.MigrateToCurrent(database.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Durable memory: two current entries, one superseded.
	pcDir := filepath.Join(ws, "personal-context")
	if err := os.MkdirAll(pcDir, 0755); err != nil {
		t.Fatal(err)
	}
	entries := `{"id":"a","status":"current","predicate":"likes","value":"tea"}
{"id":"b","status":"current","predicate":"timezone","value":"UTC"}
{"id":"c","status":"superseded","predicate":"likes","value":"coffee"}
`
	if err := os.WriteFile(filepath.Join(pcDir, "entries.jsonl"), []byte(entries), 0644); err != nil {
		t.Fatal(err)
	}

	// One live job, one completed in-window, one recovery event.
	now := time.Now()
	if _, err := database.Exec(`INSERT INTO jobs (id, kind, status, progress, created_at, updated_at)
		VALUES ('j-live','browse','running',0,?,?)`, now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO jobs (id, kind, status, progress, created_at, updated_at, finished_at)
		VALUES ('j-done','browse','succeeded',1,?,?,?)`, now.Unix(), now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO canonical_events (id, type, timestamp, status)
		VALUES ('e-int','task.interrupted',?,'interrupted')`, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO canonical_events (id, type, timestamp, status)
		VALUES ('e-vf','verification.failed',?,'failed')`, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	d := New(database.DB, &testProvider{}, nil, ws)
	d.SetRetrievalSource(func() RetrievalStats {
		return RetrievalStats{
			RAG:  RetrievalPath{Queries: 4, AvgMs: 12.5, LastMs: 9},
			Memo: RetrievalPath{Queries: 2, AvgMs: 3, LastMs: 3},
		}
	})

	res := d.checkIntelligence(context.Background())
	if res.Status != "warning" {
		t.Fatalf("a verification failure must surface as warning, got %s", res.Status)
	}
	for _, want := range []string{
		"2 durable", "1 active", "1 done in 24h", "1 recovered",
		"12ms avg over 4", "3ms avg over 2", "1 verification failure",
	} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("message missing %q: %s", want, res.Message)
		}
	}
}

// A missing store is reported as info, never an error.
func TestCheckIntelligenceNoDB(t *testing.T) {
	d := New(nil, nil, nil, "")
	res := d.checkIntelligence(context.Background())
	if res.Status != "info" {
		t.Fatalf("no-db intelligence must be info, got %s", res.Status)
	}
}

// countCurrentMemoryEntries ignores superseded rows and tolerates a missing
// file.
func TestCountCurrentMemoryEntries(t *testing.T) {
	if got := countCurrentMemoryEntries(""); got != 0 {
		t.Fatalf("empty workspace = %d", got)
	}
	ws := t.TempDir()
	if got := countCurrentMemoryEntries(ws); got != 0 {
		t.Fatalf("missing file = %d", got)
	}
	dir := filepath.Join(ws, "personal-context")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "entries.jsonl"), []byte(
		`{"status":"current"}
{"status":"superseded"}
not json
{"status":"current"}
`), 0644)
	if got := countCurrentMemoryEntries(ws); got != 2 {
		t.Fatalf("expected 2 current entries, got %d", got)
	}
}
