package appliance

import "testing"

func TestIsClean(t *testing.T) {
	if !IsClean("") || !IsClean("  \n") {
		t.Error("empty output must be clean")
	}
	if IsClean(" M cmd/ghost/main.go") {
		t.Error("a modified file makes the tree dirty")
	}
	if IsClean("?? scratch/") {
		t.Error("untracked paths make the tree dirty")
	}
}

func TestSummarizeDirty(t *testing.T) {
	porcelain := " M cmd/ghost/main.go\n?? newfile.go\nA  added.go\n"
	paths := SummarizeDirty(porcelain, 10)
	if len(paths) != 3 {
		t.Fatalf("want 3 paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "cmd/ghost/main.go" {
		t.Errorf("first path = %q", paths[0])
	}
	// Bounded.
	if got := SummarizeDirty(porcelain, 2); len(got) != 2 {
		t.Errorf("max=2 must cap at 2, got %d", len(got))
	}
}
