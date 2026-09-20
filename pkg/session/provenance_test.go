package session

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// The source channel is provenance, not conversation identity: it must
// round-trip through the store so a surface can be noted after the fact,
// while the message still lives in the shared conversation.
func TestSQLiteStoreRoundTripsSourceChannel(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := NewSQLiteStore(database)

	store.AddFullMessage("main", providers.Message{
		Role:          "user",
		Content:       "hi from telegram",
		SourceChannel: "telegram",
	})
	store.AddFullMessage("main", providers.Message{
		Role:    "assistant",
		Content: "hello",
	})

	history := store.GetHistory("main")
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].SourceChannel != "telegram" {
		t.Errorf("user provenance must survive the store, got %q", history[0].SourceChannel)
	}
	if history[1].SourceChannel != "" {
		t.Errorf("assistant message has no external surface, got %q", history[1].SourceChannel)
	}
}
