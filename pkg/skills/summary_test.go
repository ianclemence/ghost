package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedSkills(t *testing.T, names ...string) *SkillsLoader {
	t.Helper()
	ws := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(ws, "skills", n)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + n + "\ndescription: Does " + n + " things. Invoke when the user says '" + n + " now'.\n---\n\n# " + n + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewSkillsLoader(ws, "", "")
}

// The index fits the budget, names new arrivals, and stays sorted.
func TestSkillsSummaryBudget(t *testing.T) {
	sl := seedSkills(t, "zebra", "alpha", "mango")
	full := sl.BuildSkillsSummaryBudget(0)
	for _, n := range []string{"zebra", "alpha", "mango"} {
		if !strings.Contains(full, "<name>"+n+"</name>") {
			t.Fatalf("missing skill %q in %q", n, full)
		}
	}
	ia, iz := strings.Index(full, "alpha"), strings.Index(full, "zebra")
	if ia < 0 || iz < 0 || ia > iz {
		t.Fatal("summary must be name-sorted for prompt stability")
	}

	// Tiny budget: at least one skill plus an overflow pointer, never a
	// silent drop.
	tiny := sl.BuildSkillsSummaryBudget(400)
	if !strings.Contains(tiny, "<skill>") {
		t.Fatalf("budget must keep at least one skill: %q", tiny)
	}
	if !strings.Contains(tiny, "more skills installed") || !strings.Contains(tiny, "list_dir") {
		t.Fatalf("overflow must name the count and recovery path: %q", tiny)
	}
}

// The version fingerprints installs, removals, and edits.
func TestSkillsVersion(t *testing.T) {
	ws := t.TempDir()
	mk := func(n string) {
		dir := filepath.Join(ws, "skills", n)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+n+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a")
	sl := NewSkillsLoader(ws, "", "")
	v1 := sl.Version()
	if v1 == "" {
		t.Fatal("version must not be empty")
	}
	mk("b")
	if v2 := sl.Version(); v2 == v1 {
		t.Fatal("install must change the version")
	}
	v2 := sl.Version()
	if err := os.RemoveAll(filepath.Join(ws, "skills", "b")); err != nil {
		t.Fatal(err)
	}
	if v3 := sl.Version(); v3 == v2 || v3 != v1 {
		t.Fatalf("removal must restore the original version: %q vs %q vs %q", v1, v2, v3)
	}
}
