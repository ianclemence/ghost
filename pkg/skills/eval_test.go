package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEvalSkill(t *testing.T, dir, frontmatter string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\n" + frontmatter + "\n---\n\n# Skill\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestEvalPassesGoodSkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "good")
	writeEvalSkill(t, dir, "name: good\ndescription: does good things\nrequires: [web.search, memory.recall]")
	if evals := EvalSkill(dir); !EvalPassed(evals) {
		t.Fatalf("good skill failed: %+v", evals)
	}
}

func TestEvalFailsUnknownCapability(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bad")
	writeEvalSkill(t, dir, "name: bad\ndescription: wants everything\nrequires: [mind.read]")
	if evals := EvalSkill(dir); EvalPassed(evals) {
		t.Fatal("unknown capability passed eval")
	}
}

func TestEvalFailsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	if evals := EvalSkill(dir); EvalPassed(evals) {
		t.Fatal("missing SKILL.md passed eval")
	}
}

func TestEvalFailsBlockedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeEvalSkill(t, dir, "name: bin\ndescription: ships binaries")
	if err := os.WriteFile(filepath.Join(dir, "run.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	evals := EvalSkill(dir)
	for _, e := range evals {
		if e.Name == "hygiene" && !e.Passed {
			return
		}
	}
	t.Fatalf("blocked file passed hygiene: %+v", evals)
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "orig")
	writeEvalSkill(t, src, "name: s\ndescription: snapshot me")
	backup, err := snapshotSkillDir(src)
	if err != nil || backup == "" {
		t.Fatalf("snapshot failed: %v %q", err, backup)
	}
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "restored-parent", "s")
	if err := restoreSkillDir(backup, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatalf("restored skill missing SKILL.md: %v", err)
	}
}
