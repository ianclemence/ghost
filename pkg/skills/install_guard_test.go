package skills

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSkillFileBlocked(t *testing.T) {
	ok := []string{"SKILL.md", "scripts/run.py", "notes.txt", "assets/logo.svg"}
	bad := []string{"setup.sh", "../evil", "a/../../evil", "scripts/payload.exe", ".git/config", "a/../b"}
	for _, p := range ok {
		if SkillFileBlocked(p) {
			t.Errorf("expected %q allowed", p)
		}
	}
	for _, p := range bad {
		if !SkillFileBlocked(p) {
			t.Errorf("expected %q blocked", p)
		}
	}
}

func TestValidateSkillDownloadBounds(t *testing.T) {
	mk := func(files map[string]string) func(string) ([]byte, error) {
		return func(rel string) ([]byte, error) { return []byte(files[rel]), nil }
	}
	// Happy path: one SKILL.md with name+description.
	good := map[string]string{"SKILL.md": "---\nname: Demo\ndescription: A demo skill\n---\n\n" + string(make([]byte, 120))}
	if err := ValidateSkillDownloadBounds([]string{"SKILL.md"}, mk(good)); err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}
	// Missing root SKILL.md.
	if err := ValidateSkillDownloadBounds([]string{"docs/readme.md"}, mk(good)); err == nil {
		t.Fatal("missing SKILL.md must be rejected")
	}
	// Blocked binary extension.
	bin := map[string]string{"SKILL.md": "---\nname: X\ndescription: desc\n---\n\n" + string(make([]byte, 120)), "payload.sh": "echo hi"}
	if err := ValidateSkillDownloadBounds([]string{"SKILL.md", "payload.sh"}, mk(bin)); err == nil {
		t.Fatal("blocked extension must be rejected")
	}
	// Path traversal.
	if err := ValidateSkillDownloadBounds([]string{"../evil"}, mk(good)); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestProvenanceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Not present -> nil.
	if p, err := ReadProvenance(dir); err != nil || p != nil {
		t.Fatalf("expected nil provenance, got %+v err=%v", p, err)
	}
	want := Provenance{Type: "github", Owner: "ianclemence", Repo: "ghost-skills",
		Branch: "main", Path: "skills/research", CommitSHA: "abc123",
		InstalledAt: time.Now().UTC()}
	if err := WriteProvenance(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadProvenance(dir)
	if err != nil || got == nil {
		t.Fatalf("provenance read failed: %v", err)
	}
	if got.Owner != want.Owner || got.Repo != want.Repo || got.CommitSHA != want.CommitSHA {
		t.Fatalf("provenance mismatch: %+v", got)
	}
	if _, err := filepath.Abs(filepath.Join(dir, SkillSourceFile)); err != nil {
		t.Fatal(err)
	}
}

func TestSplitGitHubRepo(t *testing.T) {
	o, r := splitGitHubRepo("ianclemence/ghost-skills")
	if o != "ianclemence" || r != "ghost-skills" {
		t.Fatalf("split: %q %q", o, r)
	}
	o2, r2 := splitGitHubRepo("single")
	if o2 != "single" || r2 != "" {
		t.Fatalf("split single: %q %q", o2, r2)
	}
}
