package session

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// Compaction must bound the model's context without hiding earlier turns
// from the owner. TruncateHistory marks rows compacted: GetHistory (model
// context) drops them, GetDisplayHistory (owner transcript) keeps them.
func TestCompactionKeepsOwnerTranscript(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := NewSQLiteStore(database)
	for i := 0; i < 10; i++ {
		store.AddFullMessage("main", providers.Message{Role: "user", Content: "msg"})
		time.Sleep(time.Millisecond)
	}
	store.TruncateHistory("main", 4)

	model := store.GetHistory("main")
	if len(model) != 4 {
		t.Errorf("model context must be truncated to 4, got %d", len(model))
	}
	display := store.GetDisplayHistory("main")
	if len(display) != 10 {
		t.Errorf("owner transcript must keep all 10 rows, got %d", len(display))
	}
}
