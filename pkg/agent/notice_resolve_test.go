package agent

import (
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
