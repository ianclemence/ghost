package cevents

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	_ "modernc.org/sqlite"
)

func openTestStream(t *testing.T) *Stream {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOrderingAcrossFullTrace(t *testing.T) {
	s := openTestStream(t)
	types := []Type{MessageReceived, CapabilityStarted, PermissionRequested, PermissionApproved, CapabilityCompleted}
	for _, typ := range types {
		s.Publish(&Event{Type: typ, RequestID: "req-1", GhostID: "g1", Payload: map[string]interface{}{"t": string(typ)}})
	}
	got := s.ByRequest("req-1")
	if len(got) != len(types) {
		t.Fatalf("got %d events", len(got))
	}
	for i, typ := range types {
		if got[i].Type != typ {
			t.Fatalf("position %d: got %s want %s", i, got[i].Type, typ)
		}
		if i > 0 && got[i].Seq <= got[i-1].Seq {
			t.Fatal("sequence must be monotonic")
		}
	}
}

func TestInternalNeverSSE(t *testing.T) {
	s := openTestStream(t)
	e := s.Publish(&Event{Type: ToolStarted, RequestID: "r", Payload: map[string]interface{}{"tool": "exec"}})
	if _, _, ok := e.SSEForm(); ok {
		t.Fatal("internal tool event must never serialize to SSE")
	}
	u := s.Publish(&Event{Type: CapabilityCompleted, RequestID: "r", Status: "success"})
	if _, _, ok := u.SSEForm(); !ok {
		t.Fatal("user-visible event must serialize")
	}
}

func TestSecretsRedactedAtBoundary(t *testing.T) {
	s := openTestStream(t)
	e := s.Publish(&Event{Type: MessageCreated, RequestID: "r",
		Payload: map[string]interface{}{"api_key": "sk-proj-abcdefghijklmnop", "text": "hi"}})
	if e.Payload["api_key"] == "sk-proj-abcdefghijklmnop" {
		t.Fatal("secret must be redacted at publish")
	}
	if e.Payload["text"] != "hi" {
		t.Fatal("innocent payload rewritten")
	}
	// Persisted form is redacted too.
	got := s.ByRequest("r")
	if got[0].Payload["api_key"] == "sk-proj-abcdefghijklmnop" {
		t.Fatal("persisted payload must be redacted")
	}
}

func TestTransientNotPersisted(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: AgentProgress, RequestID: "r", Payload: map[string]interface{}{"note": "working"}})
	if got := s.ByRequest("r"); len(got) != 0 {
		t.Fatal("transient progress must not hit SQLite")
	}
}

func TestListenerIsolation(t *testing.T) {
	s := openTestStream(t)
	var hits int
	s.Subscribe(Filter{}, func(e *Event) { hits++ })
	s.Subscribe(Filter{}, func(e *Event) { panic("bad listener") })
	s.Publish(&Event{Type: GhostReady, GhostID: "g"})
	if hits != 1 {
		t.Fatal("panicking listener must not break delivery")
	}
}

func TestOwnerIsolation(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: MessageCreated, GhostID: "ghost-a"})
	s.Publish(&Event{Type: MessageCreated, GhostID: "ghost-b"})
	var seen []string
	unsub := s.Subscribe(Filter{GhostID: "ghost-a"}, func(e *Event) { seen = append(seen, e.GhostID) })
	defer unsub()
	s.Publish(&Event{Type: MessageCreated, GhostID: "ghost-a"})
	s.Publish(&Event{Type: MessageCreated, GhostID: "ghost-b"})
	for _, g := range seen {
		if g != "ghost-a" {
			t.Fatal("cross-ghost leak in subscription")
		}
	}
	got := s.Recent(10, Filter{GhostID: "ghost-b", UserVisibleOnly: true})
	for _, e := range got {
		if e.GhostID != "ghost-b" || !e.Visibility.UserVisible() {
			t.Fatal("filtered query leaked")
		}
	}
}

func TestVisibilityDefault(t *testing.T) {
	if AgentProgress.DefaultVisibility() != product.VisInternalTrace {
		t.Fatal("progress must default internal")
	}
	if PermissionRequested.DefaultVisibility() != product.VisUserMessage {
		t.Fatal("permission request must default user-visible")
	}
}

func TestPrune(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: ToolStarted, RequestID: "old"})
	s.Publish(&Event{Type: CapabilityCompleted, RequestID: "old"})
	// Transient tool.started isn't persisted at all; nothing to prune young.
	if n := s.Prune(time.Hour); n != 0 {
		t.Fatalf("nothing old enough: pruned %d", n)
	}
}

func TestSinceResume(t *testing.T) {
	s := openTestStream(t)
	s.Publish(&Event{Type: MessageCreated, RequestID: "a", GhostID: "g"})
	s.Publish(&Event{Type: MessageCreated, RequestID: "b", GhostID: "g"})
	s.Publish(&Event{Type: MessageCreated, RequestID: "c", GhostID: "g"})
	all := s.Recent(10, Filter{GhostID: "g"})
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// Client saw the first two (seq order); resume from cursor.
	var cursor int64
	for _, e := range all {
		if e.RequestID == "b" {
			cursor = e.Seq
		}
	}
	got := s.Since(cursor, 10, Filter{GhostID: "g"})
	if len(got) != 1 || got[0].RequestID != "c" {
		t.Fatalf("resume must return only newer: %+v", got)
	}
	// Unknown ghost sees nothing.
	if got := s.Since(0, 10, Filter{GhostID: "other"}); len(got) != 0 {
		t.Fatal("cross-owner resume must be empty")
	}
}
