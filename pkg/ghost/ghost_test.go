package ghost

import (
	"testing"
)

func TestIdentityMintedOnce(t *testing.T) {
	ws := t.TempDir()
	s, err := Open(ws, "", "Ghost", "Ian")
	if err != nil {
		t.Fatal(err)
	}
	g := s.GhostEntity()
	if g.ID == "" || g.Name != "Ghost" {
		t.Fatalf("bad entity: %+v", g)
	}
	o := s.OwnerEntity()
	if o.ID == "" || o.DisplayName != "Ian" || g.OwnerID != o.ID {
		t.Fatalf("bad owner link: %+v %+v", g, o)
	}
	// Reopen adopts the same identity (restart-safe, never reminted).
	s2, err := Open(ws, "", "Other", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if s2.GhostEntity().ID != g.ID {
		t.Fatal("ghost_id must survive restart")
	}
	if s2.OwnerEntity().ID != o.ID {
		t.Fatal("owner must survive restart")
	}
}

func TestAdoptsProvidedID(t *testing.T) {
	s, err := Open(t.TempDir(), "ghost-abc", "G", "O")
	if err != nil {
		t.Fatal(err)
	}
	if s.GhostEntity().ID != "ghost-abc" {
		t.Fatal("must adopt ghoststate identity id")
	}
}

func TestRenameAndStatus(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "Ghost", "Ian")
	if err := s.Rename("  "); err == nil {
		t.Fatal("blank rename must fail")
	}
	if err := s.Rename("Maus"); err != nil {
		t.Fatal(err)
	}
	if s.GhostEntity().Name != "Maus" {
		t.Fatal("rename must persist")
	}
	if err := s.SetStatus(StatusReady); err != nil {
		t.Fatal(err)
	}
	if s.GhostEntity().Status != StatusReady {
		t.Fatal("status must persist")
	}
}

func TestTimezoneValidation(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "G", "O")
	if err := s.SetTimezone("Not/AZone"); err == nil {
		t.Fatal("invalid timezone must fail")
	}
	if err := s.SetTimezone("Asia/Bangkok"); err != nil {
		t.Fatal(err)
	}
	if s.GhostEntity().Timezone != "Asia/Bangkok" {
		t.Fatal("timezone must persist")
	}
}

func TestPrimaryAgentInvariant(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "G", "O")
	p := s.PrimaryAgent()
	if !p.IsPrimary || p.Kind != "main" || p.GhostID != s.GhostEntity().ID {
		t.Fatalf("bad primary: %+v", p)
	}
	// Second primary rejected; non-primary extension allowed.
	if err := s.RegisterAgent(Agent{ID: "a2", Kind: "main", IsPrimary: true}); err == nil {
		t.Fatal("second primary must be rejected")
	}
	if err := s.RegisterAgent(Agent{ID: "work", Kind: "work", DisplayName: "Work"}); err != nil {
		t.Fatalf("future agent must register: %v", err)
	}
	if len(s.Agents()) != 2 {
		t.Fatal("must list both agents")
	}
	if err := s.RegisterAgent(Agent{ID: "work", Kind: "work"}); err == nil {
		t.Fatal("duplicate agent must be rejected")
	}
}

func TestConversationsBelongToGhost(t *testing.T) {
	// Entity identity is independent of any session: two stores on
	// different workspaces are different Ghosts; sessions never mint
	// identity. (Conversation↔Ghost linkage is asserted by the
	// caller's session key carrying the ghost id; here we assert the
	// entity side of that contract.)
	a, _ := Open(t.TempDir(), "", "G", "O")
	b, _ := Open(t.TempDir(), "", "G", "O")
	if a.GhostEntity().ID == b.GhostEntity().ID {
		t.Fatal("distinct workspaces must be distinct Ghosts")
	}
}
