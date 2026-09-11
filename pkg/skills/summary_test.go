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

func TestParseListField(t *testing.T) {
	m := map[string]string{
		"commands":     "[python, curl]",
		"requires_env": "[OPENWEATHER_API_KEY, EXTRA]",
		"empty":        "[]",
	}
	if got := parseListField(m, "commands"); len(got) != 2 || got[0] != "python" || got[1] != "curl" {
		t.Fatalf("commands parse: %v", got)
	}
	if got := parseListField(m, "requires_env"); len(got) != 2 || got[0] != "OPENWEATHER_API_KEY" {
		t.Fatalf("env parse: %v", got)
	}
	if got := parseListField(m, "empty"); got != nil {
		t.Fatalf("empty list must be nil: %v", got)
	}
	if got := parseListField(m, "missing"); got != nil {
		t.Fatalf("missing key must be nil: %v", got)
	}
}

// A skill whose declared fallback binary is absent renders a fallback note;
// one whose requirements are satisfied does not.
func TestRequirementGatingNote(t *testing.T) {
	present := "sh"
	absent := "ghost-definitely-not-a-real-binary-xyz"

	okInfo := SkillInfo{Name: "ok", RequiresBins: []string{present}}
	if note := requirementNote(okInfo); note != "" {
		t.Fatalf("satisfied requirement must not note: %q", note)
	}

	badInfo := SkillInfo{Name: "bad", RequiresBins: []string{absent + " --flag"}}
	note := requirementNote(badInfo)
	if !strings.Contains(note, absent) || !strings.Contains(note, "fallback unavailable") {
		t.Fatalf("unmet requirement must note: %q", note)
	}
	if strings.Contains(note, "--flag") {
		t.Fatalf("note must name the binary, not its flags: %q", note)
	}
}

// The frontmatter parser surfaces nested prerequisites into RequiresBins so
// the gating note has data to work with.
func TestMetadataParsesPrerequisites(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "skills", "needsbin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: needsbin\ndescription: x\nprerequisites:\n  commands: [ghost-nope-binary]\nrequires_env: [GHOST_TEST_ENV_VAR]\n---\n\n# x\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	sl := NewSkillsLoader(ws, "", "")
	infos := sl.ListSkills()
	if len(infos) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(infos))
	}
	if len(infos[0].RequiresBins) != 1 || infos[0].RequiresBins[0] != "ghost-nope-binary" {
		t.Fatalf("requires bins not parsed: %+v", infos[0].RequiresBins)
	}
	if len(infos[0].RequiresEnv) != 1 || infos[0].RequiresEnv[0] != "GHOST_TEST_ENV_VAR" {
		t.Fatalf("requires env not parsed: %+v", infos[0].RequiresEnv)
	}
	// And the note renders through the summary.
	summary := sl.BuildSkillsSummaryBudget(0)
	if !strings.Contains(summary, "<fallback>") || !strings.Contains(summary, "ghost-nope-binary") {
		t.Fatalf("summary must carry the fallback note: %q", summary)
	}
}

// A skill may declare capability requirements, but that is a request, not
// a grant: the summary surfaces them and nothing here authorizes them.
func TestSkillDeclaresCapabilityRequirements(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "skills", "scheduler")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: scheduler\ndescription: x\nrequires: [calendar.read, web.search]\n---\n\n# x\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	sl := NewSkillsLoader(ws, "", "")
	infos := sl.ListSkills()
	if len(infos) != 1 || len(infos[0].RequiresCapabilities) != 2 {
		t.Fatalf("requires not parsed: %+v", infos)
	}
	summary := sl.BuildSkillsSummaryBudget(0)
	if !strings.Contains(summary, "<requires>calendar.read, web.search</requires>") {
		t.Fatalf("summary must surface declared capabilities: %q", summary)
	}
}
