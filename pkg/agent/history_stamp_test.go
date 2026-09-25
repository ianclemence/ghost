package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// Old prose must arrive dated: a bare "remind me tomorrow" written days
// ago is undatable on its own, and stale relative words once lived two
// days in the owner's chat before the model caught itself. Tool payloads
// and unknown-time messages stay byte-exact.
func TestStampHistoryDatesOldProse(t *testing.T) {
	at := mustParse(t, "2026-09-23T15:45:00+07:00")
	at2 := mustParse(t, "2026-09-23T16:02:00+07:00")
	history := []providers.Message{
		{Role: "user", Content: "remind me tomorrow at 9", CreatedAt: at},
		{Role: "assistant", Content: "Set for Thursday, Sep 24 at 9:00", CreatedAt: at2},
		{Role: "tool", Content: "raw payload", CreatedAt: at},
		{Role: "user", Content: "no known time"},
	}
	got := stampHistory(history)

	want := []string{
		"[2026-09-23 15:45] remind me tomorrow at 9",
		"[2026-09-23 16:02] Set for Thursday, Sep 24 at 9:00",
		"raw payload",
		"no known time",
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("msg %d = %q, want %q", i, got[i].Content, w)
		}
	}
}

// The turn being sent now carries no stamp of its own — its clock lives
// in ## Current Time — while the history around it does.
func TestBuildMessagesStampsHistoryNotCurrentMessage(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())
	at := mustParse(t, "2026-09-23T15:45:00+07:00")
	history := []providers.Message{{Role: "user", Content: "the old ask", CreatedAt: at}}

	msgs := cb.BuildMessages(context.Background(), history, "", "fresh ask", nil, "", "", nil, nil)

	if len(msgs) < 3 {
		t.Fatalf("want system + history + current, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "## Current Time") {
		t.Fatal("first message must be the system prompt carrying ## Current Time")
	}

	var stamped bool
	for _, m := range msgs {
		if m.Content == "[2026-09-23 15:45] the old ask" {
			stamped = true
		}
		if m.Content == "the old ask" {
			t.Error("history line must reach the model stamped")
		}
	}
	if !stamped {
		t.Error("stamped history line missing from built messages")
	}
	if cur := msgs[len(msgs)-1]; cur.Content != "fresh ask" {
		t.Errorf("current message must stay unstamped, got %q", cur.Content)
	}
}
