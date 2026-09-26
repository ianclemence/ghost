package agent

import (
	"database/sql"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/schema"
	_ "modernc.org/sqlite"
)

func usageStream(t *testing.T) (*cevents.Stream, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Production migrates before any subsystem runs; mirror that.
	if _, err := schema.MigrateToCurrent(db); err != nil {
		t.Fatal(err)
	}
	s, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

func TestRecordTurnUsage(t *testing.T) {
	s, db := usageStream(t)
	al := &AgentLoop{governance: &Governance{Events: s, GhostID: "g", AgentID: "a"}}
	opts := processOptions{SessionKey: "sess-1", RequestID: "req-1"}
	// Measured turn: exact dollars recorded.
	recordTurnUsage(al, opts, "deepseek:deepseek-flash", 2, 1000, 500, 1500, 0.004, 2, 0, "measured")
	var cost float64
	var unknown int64
	var model string
	err := db.QueryRow(`SELECT json_extract(payload,'$.cost_usd'), json_extract(payload,'$.cost_unknown'), json_extract(payload,'$.model') FROM canonical_events WHERE type='usage.recorded'`).Scan(&cost, &unknown, &model)
	if err != nil {
		t.Fatalf("usage row must persist: %v", err)
	}
	if cost != 0.004 || unknown != 0 || model != "deepseek-flash" {
		t.Fatalf("row wrong: %v %v %v", cost, unknown, model)
	}
	// Unpriced provider: unknown recorded, never zero-claimed.
	recordTurnUsage(al, opts, "mystery:m9", 1, 100, 50, 150, 0, 1, 1, "measured")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM canonical_events WHERE type='usage.recorded'`).Scan(&n)
	if n != 2 {
		t.Fatalf("want 2 rows, got %d", n)
	}
	var unk int64
	db.QueryRow(`SELECT json_extract(payload,'$.cost_unknown') FROM canonical_events WHERE type='usage.recorded' ORDER BY seq DESC LIMIT 1`).Scan(&unk)
	if unk == 0 {
		t.Fatal("unpriced turn must record unknown")
	}
	// Zero-usage turn writes nothing; nil governance never panics.
	recordTurnUsage(al, opts, "x:y", 0, 0, 0, 0, 0, 0, 0, "unknown")
	recordTurnUsage(nil, opts, "x:y", 1, 1, 1, 2, 0, 1, 1, "measured")
	recordTurnUsage(&AgentLoop{}, opts, "x:y", 1, 1, 1, 2, 0, 1, 1, "measured")
	db.QueryRow(`SELECT COUNT(*) FROM canonical_events WHERE type='usage.recorded'`).Scan(&n)
	if n != 2 {
		t.Fatalf("empty/nil turns must not record, got %d", n)
	}
}
