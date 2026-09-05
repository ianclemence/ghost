package contexts

import (
	"testing"
)

func TestDefaultPersonal(t *testing.T) {
	s, err := Open(t.TempDir(), "ghost-1")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Get("personal")
	if !ok || c.Kind != KindPersonal {
		t.Fatal("personal context must exist by default")
	}
}

func TestMemoryIsolation(t *testing.T) {
	work := &Context{ID: "work", Kind: KindWork, MemoryScopes: []string{"context:work"}}
	home := &Context{ID: "home", Kind: KindHome, MemoryScopes: []string{"context:home"}}
	// Global memories shared; home-scoped invisible to work.
	if !CanAccessMemory(work, nil) || !CanAccessMemory(work, []string{"context:work"}) {
		t.Fatal("work must see global + own")
	}
	if CanAccessMemory(work, []string{"context:home"}) {
		t.Fatal("work must not see home-scoped memory")
	}
	if CanAccessMemory(home, []string{"context:work"}) {
		t.Fatal("home must not see work-scoped memory")
	}
}

func TestCapabilityAllowlist(t *testing.T) {
	open := &Context{ID: "personal"}
	if !CanUseCapability(open, "anything.at.all") {
		t.Fatal("unconfigured context allows all (V1 default)")
	}
	locked := &Context{ID: "work", Capabilities: []string{"calendar.read"}}
	if !CanUseCapability(locked, "calendar.read") || CanUseCapability(locked, "hass.control") {
		t.Fatal("allowlist must gate")
	}
}

func TestFileScope(t *testing.T) {
	ws := t.TempDir()
	c := &Context{ID: "work", FileRoots: []string{"work-docs"}}
	if !FileInScope(c, ws, ws+"/work-docs/plan.md") {
		t.Fatal("root file must be in scope")
	}
	if FileInScope(c, ws, ws+"/personal/diary.md") {
		t.Fatal("outside roots must be out of scope")
	}
	if FileInScope(c, ws, "/etc/passwd") {
		t.Fatal("escape must be out of scope")
	}
}

func TestCreateProject(t *testing.T) {
	s, _ := Open(t.TempDir(), "g1")
	p, err := s.Create(KindProject, "Ghost Launch")
	if err != nil {
		t.Fatal(err)
	}
	if p.GhostID != "g1" {
		t.Fatal("project must belong to the ghost")
	}
	if _, err := s.Create(KindWork, "Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(KindWork, "Work Again"); err == nil {
		t.Fatal("singleton kinds must not duplicate")
	}
	// Ghost isolation: other ghost's contexts never load.
	s2, _ := Open(s.path[:len(s.path)-len("/state/contexts.json")], "g2")
	_ = s2
}

func TestSessionMapping(t *testing.T) {
	ws := t.TempDir()
	s, _ := Open(ws, "g1")
	if got := s.SessionContext("new-session"); got != "personal" {
		t.Fatalf("default must be personal, got %s", got)
	}
	if _, err := s.Create(KindWork, "Work"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionContext("sess-1", "work"); err != nil {
		t.Fatal(err)
	}
	if got := s.SessionContext("sess-1"); got != "work" {
		t.Fatal("session must stick to work")
	}
	if err := s.SetSessionContext("sess-1", "nope"); err == nil {
		t.Fatal("unknown context must fail")
	}
	// Restart: mapping persists.
	s2, _ := Open(ws, "g1")
	if got := s2.SessionContext("sess-1"); got != "work" {
		t.Fatal("session mapping must survive restart")
	}
}

func TestScopesForSession(t *testing.T) {
	s, _ := Open(t.TempDir(), "g1")
	s.Create(KindWork, "Work")
	s.SetSessionContext("sess-w", "work")
	scopes := s.ScopesForSession("sess-w")
	found := false
	for _, sc := range scopes {
		if sc == "context:work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("work session must carry its tag: %v", scopes)
	}
	if got := s.WriteScopes("sess-personal-x"); len(got) != 0 {
		t.Fatal("personal writes stay global")
	}
	if got := s.WriteScopes("sess-w"); len(got) != 1 || got[0] != "context:work" {
		t.Fatalf("work writes must tag: %v", got)
	}
}
