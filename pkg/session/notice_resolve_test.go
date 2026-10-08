package session

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// An alert that reported a condition is marked resolved when the condition
// clears: the words stay, only that alert is touched, and it happens once.
func TestResolveNoticeSettlesOnlyThatAlert(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	st := NewSQLiteStore(database)
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "almost out of storage", Kind: "alert", NoticeKey: "storage-critical"})
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "pod is hot", Kind: "alert", NoticeKey: "pod-hot"})
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "just a reply"})

	ids := st.ResolveNotice("main", "storage-critical")
	if len(ids) != 1 {
		t.Fatalf("resolved %d messages, want 1", len(ids))
	}
	if again := st.ResolveNotice("main", "storage-critical"); len(again) != 0 {
		t.Fatalf("resolved twice: %v", again)
	}
	if other := st.ResolveNotice("other", "pod-hot"); len(other) != 0 {
		t.Fatalf("resolved in another conversation: %v", other)
	}

	var resolved, open int
	rows, err := database.DB.Query(`SELECT COALESCE(json_extract(meta,'$.resolved'),0) FROM messages WHERE json_extract(meta,'$.notice_key') IS NOT NULL`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r int
		_ = rows.Scan(&r)
		if r == 1 {
			resolved++
		} else {
			open++
		}
	}
	if resolved != 1 || open != 1 {
		t.Fatalf("resolved=%d open=%d, want 1 and 1", resolved, open)
	}
	// The words are still there, and the key survives a reload.
	hist := st.GetDisplayHistory("main")
	found := false
	for _, m := range hist {
		if m.Content == "almost out of storage" && m.NoticeKey == "storage-critical" {
			found = true
		}
	}
	if !found {
		t.Fatal("the alert's words or key were lost")
	}
}

// Alerts from before keys existed are keyed by their opening words, once, and
// nothing else is touched.
func TestTagNoticesKeysOlderAlertsOnly(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	st := NewSQLiteStore(database)
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "I'm almost out of storage: 1 GB left.", Kind: "alert"})
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "I'm almost out of storage: said in a plain reply."})
	st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "Storage is getting low: 9 GB", Kind: "notice", NoticeKey: "storage-low"})

	if n := st.TagNotices("main", "I'm almost out of storage:", "storage-critical"); n != 1 {
		t.Fatalf("tagged %d, want 1 (only the alert)", n)
	}
	if n := st.TagNotices("main", "I'm almost out of storage:", "storage-critical"); n != 0 {
		t.Fatalf("tagged again: %d", n)
	}
	if n := st.TagNotices("main", "Storage is getting low:", "other-key"); n != 0 {
		t.Fatalf("overwrote an existing key: %d", n)
	}
	if ids := st.ResolveNotice("main", "storage-critical"); len(ids) != 1 {
		t.Fatalf("the older alert is not resolvable: %v", ids)
	}
}
