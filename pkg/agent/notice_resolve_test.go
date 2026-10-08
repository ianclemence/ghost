package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The whole path an alert takes: said once and stored with its key; then, when
// the condition clears, marked resolved, every surface told, and the same
// condition coming back is said again (the cooldown is forgotten).
func TestAlertIsRetractedWhenItsConditionClears(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	ch, unsub := al.Bus().SubscribeOutbound("t", false, 16)
	defer unsub()

	if !al.Announce("storage-critical", "I'm almost out of storage: 1 GB left.", 12*time.Hour, true) {
		t.Fatal("the alert must be said")
	}
	<-ch // the announcement itself

	var stored bool
	for _, h := range al.sessions.GetDisplayHistory("main") {
		if h.Kind == "alert" && h.NoticeKey == "storage-critical" {
			stored = true
		}
	}
	if !stored {
		t.Fatal("the alert must be stored with its kind and its key")
	}

	if al.ResolveNotice("pod-hot") {
		t.Fatal("resolved an alert that was never said")
	}
	if !al.ResolveNotice("storage-critical") {
		t.Fatal("the open alert was not resolved")
	}
	select {
	case m := <-ch:
		if m.Metadata["type"] != "notice_resolved" || m.Metadata["key"] != "storage-critical" || m.Metadata["session_id"] != "main" {
			t.Fatalf("surfaces were told the wrong thing: %+v", m.Metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("no surface was told the alert is resolved")
	}
	if al.ResolveNotice("storage-critical") {
		t.Fatal("resolved twice")
	}

	// The condition returns inside the old 12-hour cooldown: the owner is told.
	if !al.Announce("storage-critical", "I'm almost out of storage: 1 GB left.", 12*time.Hour, true) {
		t.Fatal("a returning condition must be announced again")
	}
}

// A follow-up on a canvas keeps the canvas tool, for half an hour.
func TestRecentCanvasWorkKeepsTheToolForFollowUps(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)
	if al.recentCanvasWork() {
		t.Fatal("no canvas yet")
	}
	dir := filepath.Join(ws, "canvas")
	_ = os.MkdirAll(dir, 0o755)
	f := filepath.Join(dir, "pong-v1.html")
	_ = os.WriteFile(f, []byte("<p>x</p>"), 0o644)
	if !al.recentCanvasWork() {
		t.Fatal("a canvas made just now must count")
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(f, old, old)
	if al.recentCanvasWork() {
		t.Fatal("a canvas from two hours ago must not")
	}
}
