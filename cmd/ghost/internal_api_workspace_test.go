package main

import "testing"

func TestWorkspaceFileProtected(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"notes/plan.md", false},
		{"data/captures.md", false},
		{"tmp/browser/x.png", false},
		{"memory/MEMORY.md", false},
		{"memory/202609/20260903.md", false},
		{"knowledge/notes/ref.md", false},
		{"personal-context", true},
		{"personal-context/entries.jsonl", true},
		{"personal-context/semantic-cache.json", true},
		{"state/contexts.json", true},
		{"state/session-context.json", true},
		{"events/2026-09-09.ndjson", true},
		{"knowledge/self/user-profile.md", true},
		{"knowledge/self/curated-memory.md", true},
		{"knowledge/self/contexts/work/user-profile.md", true},
		{"ghost.db", true},
		{"ghost.db-wal", true},
		{"x.sqlite", true},
		{"notes/ghost.db.bak.txt", false},
	}
	for _, c := range cases {
		if got := workspaceFileProtected(c.rel); got != c.want {
			t.Errorf("workspaceFileProtected(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

// The raw memory journal and runtime DB must never be reachable through the
// generic workspace-file surface, even by a valid device credential.
func TestWorkspaceFileProtectedBlocksInternalEstate(t *testing.T) {
	for _, rel := range []string{"personal-context/entries.jsonl", "ghost.db", "knowledge/self/contexts/work/user-profile.md"} {
		if !workspaceFileProtected(rel) {
			t.Errorf("internal estate path %q must be protected", rel)
		}
	}
}
