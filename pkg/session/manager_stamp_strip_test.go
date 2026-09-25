package session

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
)

// The write boundary must never store a model-written history label: a
// label in storage leaks into transcripts AND doubles up when the
// context builder stamps history for the next turn. Owner (user) text
// and mid-text brackets stay byte-exact — only a leading label on an
// assistant reply is bookkeeping.
func TestManagerStripsAssistantHistoryLabel(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := NewSQLiteStore(database)
	mgr := NewSessionManager(store, nil)

	mgr.AddMessage("a", "assistant", "[2026-09-25 15:59] Set — dinner Friday")
	mgr.AddMessage("u", "user", "[2026-09-25 15:59] owner words stay as typed")
	mgr.AddMessage("m", "assistant", "kept [2026-09-25 15:59] mid-text bracket")
	mgr.AddMessage("c", "assistant", "clean reply")

	cases := map[string]string{
		"a": "Set — dinner Friday",
		"u": "[2026-09-25 15:59] owner words stay as typed",
		"m": "kept [2026-09-25 15:59] mid-text bracket",
		"c": "clean reply",
	}
	for key, want := range cases {
		hist := store.GetHistory(key)
		if len(hist) != 1 {
			t.Fatalf("session %s: want 1 row, got %d", key, len(hist))
		}
		if hist[0].Content != want {
			t.Errorf("session %s content = %q, want %q", key, hist[0].Content, want)
		}
	}
}

// The JSONL store gets the same write-boundary guarantee: the strip
// lives in the manager, before either store sees the message.
func TestManagerStripsAssistantLabelJSONL(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	mgr := NewSessionManager(store, nil)

	mgr.AddMessage("j", "assistant", "[2026-09-25 15:59] Said")

	hist := store.GetHistory("j")
	if len(hist) != 1 || hist[0].Content != "Said" {
		t.Fatalf("jsonl assistant label not stripped: %+v", hist)
	}
}
