package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkillTree(t *testing.T, dir string, extra map[string]string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: Solo\ndescription: demo\n---\n\n"+string(make([]byte, 120))), 0o644)
	os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print(1)"), 0o644)
	for rel, content := range extra {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
}

func TestFindSkillRootAndValidate(t *testing.T) {
	// Empty / no skill.
	empty := t.TempDir()
	if _, err := FindSkillRoot(empty); err == nil {
		t.Fatal("archive with no skill must be refused")
	}
	// Ambiguous: two skills.
	amb := t.TempDir()
	writeSkillTree(t, filepath.Join(amb, "alpha"), nil)
	writeSkillTree(t, filepath.Join(amb, "beta"), nil)
	if _, err := FindSkillRoot(amb); err == nil {
		t.Fatal("ambiguous multi-skill archive must be refused")
	}
	// Single skill: found and validated.
	ok := t.TempDir()
	writeSkillTree(t, filepath.Join(ok, "solo"), nil)
	root, err := FindSkillRoot(ok)
	if err != nil {
		t.Fatalf("single skill not found: %v", err)
	}
	if filepath.Base(root) != "solo" {
		t.Fatalf("unexpected root %q", root)
	}
	if err := ValidateInstalledSkillDir(root); err != nil {
		t.Fatalf("valid installed skill rejected: %v", err)
	}
	// Blocked binary in the extracted skill is rejected.
	writeSkillTree(t, filepath.Join(ok, "solo"), map[string]string{"payload.sh": "echo hi"})
	if err := ValidateInstalledSkillDir(root); err == nil {
		t.Fatal("blocked binary must be rejected from an installed skill")
	}
	// Missing SKILL.md at the found root is invalid.
	os.Remove(filepath.Join(root, "SKILL.md"))
	if err := ValidateInstalledSkillDir(root); err == nil {
		t.Fatal("missing SKILL.md must be rejected")
	}
}
