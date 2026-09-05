package skills

import (
	"strings"
	"testing"
	"time"
)

func TestDurablePendingRoundtrip(t *testing.T) {
	ws := t.TempDir()
	s := NewPendingStore(ws)
	r := s.Create("sess-1", "calendar.read", "calendar", "authorization", "Connect?", "Show me my calendar tomorrow", time.Minute, map[string]string{"step": "await_auth"})
	if r.ID == "" || r.Status != PendingOpen {
		t.Fatalf("bad request: %+v", r)
	}
	got, ok := s.OpenForSession("sess-1")
	if !ok || got.ID != r.ID {
		t.Fatal("must retrieve open request")
	}
	// Restart: new store on same workspace.
	s2 := NewPendingStore(ws)
	got2, ok := s2.OpenForSession("sess-1")
	if !ok || got2.ID != r.ID || got2.Intent != "Show me my calendar tomorrow" {
		t.Fatalf("must survive restart: %+v %v", got2, ok)
	}
	if !s2.Complete(r.ID) {
		t.Fatal("complete must work")
	}
	if _, ok := s2.OpenForSession("sess-1"); ok {
		t.Fatal("completed must not be open")
	}
}

func TestPendingExpiry(t *testing.T) {
	ws := t.TempDir()
	s := NewPendingStore(ws)
	r := s.Create("sess", "cap", "skill", "", "", "intent", 50*time.Millisecond, nil)
	_ = r
	time.Sleep(60 * time.Millisecond)
	if _, ok := s.OpenForSession("sess"); ok {
		t.Fatal("expired request must not be open")
	}
}

func TestPendingCancel(t *testing.T) {
	ws := t.TempDir()
	s := NewPendingStore(ws)
	r := s.Create("sess", "cap", "skill", "", "", "intent", time.Minute, nil)
	if !s.Cancel(r.ID) {
		t.Fatal("cancel must work")
	}
	if _, ok := s.OpenForSession("sess"); ok {
		t.Fatal("cancelled must not be open")
	}
}

func TestSanitizeIntent(t *testing.T) {
	in := "check calendar tomorrow api_key: SECRET123 and bearer abcDEF123"
	out := SanitizeIntent(in)
	if strings.Contains(out, "SECRET123") || strings.Contains(out, "abcDEF123") {
		t.Fatalf("secrets must be redacted: %q", out)
	}
	s := NewPendingStore(t.TempDir())
	r := s.Create("sess", "cap", "skill", "", "", "my password: hunter2 please", time.Minute, nil)
	if strings.Contains(r.Intent, "hunter2") {
		t.Fatalf("stored intent must be sanitized: %q", r.Intent)
	}
}

func TestPendingIDsUnique(t *testing.T) {
	s := NewPendingStore(t.TempDir())
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		r := s.Create("sess", "c", "sk", "", "", "i", time.Minute, nil)
		if seen[r.ID] {
			t.Fatal("duplicate request ID")
		}
		seen[r.ID] = true
	}
}
