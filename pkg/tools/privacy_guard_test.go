package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func testGuard(ws string) ScopeGuard {
	return ScopeGuard{
		Workspace: ws,
		ContextOf: func(sessionKey string) string {
			if sessionKey == "sess-work" {
				return "work"
			}
			return "personal"
		},
	}
}

// Dated daily notes mix every context's journal: raw reads are denied and
// routed to the scope-filtered memory_recall, while listings, writes, and
// the user's own MEMORY.md keep working.
func TestScopeGuardDatedNotes(t *testing.T) {
	ws := t.TempDir()
	note := filepath.Join(ws, "memory", "202609", "20260910.md")
	if err := os.MkdirAll(filepath.Dir(note), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("journal"), 0644); err != nil {
		t.Fatal(err)
	}
	mem := filepath.Join(ws, "memory", "MEMORY.md")
	if err := os.WriteFile(mem, []byte("long term"), 0644); err != nil {
		t.Fatal(err)
	}
	g := testGuard(ws)

	if err := g.Check("sess-home", OpRead, note); err == nil {
		t.Fatal("raw read of a dated note must be denied")
	}
	if err := g.Check("sess-work", OpRead, note); err == nil {
		t.Fatal("raw read denied even for the owning context (mixed file cannot be shown raw)")
	}
	if err := g.Check("sess-home", OpRead, mem); err != nil {
		t.Fatalf("MEMORY.md stays readable: %v", err)
	}
	if err := g.Check("sess-home", OpList, filepath.Join(ws, "memory")); err != nil {
		t.Fatalf("listing note names stays allowed: %v", err)
	}
	if err := g.Check("sess-work", OpWrite, note); err != nil {
		t.Fatalf("journal writes must keep working: %v", err)
	}
}

// Existing estate protections keep their behavior under the op parameter.
func TestScopeGuardLegacyEstate(t *testing.T) {
	ws := t.TempDir()
	for _, p := range []string{"personal-context/entries.jsonl", "state/turns/x.json"} {
		full := filepath.Join(ws, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	g := testGuard(ws)
	for _, op := range []Op{OpRead, OpWrite, OpList} {
		if err := g.Check("s", op, filepath.Join(ws, "personal-context", "entries.jsonl")); err == nil {
			t.Fatalf("personal-context must be denied for op %q", op)
		}
	}
	// Inert without a resolver: everything permitted (legacy workspaces).
	inert := ScopeGuard{Workspace: ws}
	if err := inert.Check("s", OpRead, filepath.Join(ws, "memory", "202609", "20260910.md")); err != nil {
		t.Fatalf("inert guard must permit: %v", err)
	}
}
