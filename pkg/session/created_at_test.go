package session

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// History must carry when each line was said. Without it a "later today"
// from a week ago is undatable on its own, and stale relative words
// survive into the model's context (observed: "tomorrow (Thu, Sep 24)"
// still standing in a summary read back on Sep 25).
func TestHistoryLoadsMessageTimestamps(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := NewSQLiteStore(database)

	before := time.Now().Add(-time.Second)
	store.AddFullMessage("main", providers.Message{Role: "user", Content: "remind me tomorrow"})
	after := time.Now().Add(time.Second)

	hist := store.GetHistory("main")
	if len(hist) != 1 {
		t.Fatalf("want 1 history row, got %d", len(hist))
	}
	if hist[0].CreatedAt.IsZero() {
		t.Fatal("loaded history must carry CreatedAt")
	}
	if hist[0].CreatedAt.Before(before) || hist[0].CreatedAt.After(after) {
		t.Errorf("CreatedAt %v outside the write window", hist[0].CreatedAt)
	}

	// The store never stamps content itself — stamping is a context-time
	// decision, so the owner transcript stays byte-exact.
	if hist[0].Content != "remind me tomorrow" {
		t.Errorf("model history content must stay unstamped in the store, got %q", hist[0].Content)
	}
	display := store.GetDisplayHistory("main")
	if len(display) != 1 || display[0].Content != "remind me tomorrow" {
		t.Errorf("owner transcript must stay unstamped, got %+v", display)
	}
}

// Same guarantee for the JSONL store: its entries already record a write
// time, and reading history must surface it.
func TestJSONLHistoryLoadsMessageTimestamps(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	store.AddFullMessage("main", providers.Message{Role: "user", Content: "later today"})

	hist := store.GetHistory("main")
	if len(hist) != 1 {
		t.Fatalf("want 1 history row, got %d", len(hist))
	}
	if hist[0].CreatedAt.IsZero() {
		t.Fatal("jsonl history must carry CreatedAt")
	}
}
