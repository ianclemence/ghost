package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readSkill(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSyncSeedsNewSkills(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "A")
	writeSkill(t, bundled, "beta", "B")

	report, err := SyncBundled(bundled, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Seeded) != 2 {
		t.Fatalf("expected 2 seeded, got %v", report.Seeded)
	}
	if readSkill(t, runtime, "alpha") != "A" || readSkill(t, runtime, "beta") != "B" {
		t.Fatal("skills not copied to runtime")
	}
	m, err := LoadManifest(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if m.Skills["alpha"].Origin == "" || m.Skills["beta"].Origin == "" {
		t.Fatal("origin hashes not recorded")
	}
	if m.Skills["alpha"].UserModified {
		t.Fatal("freshly seeded skill should not be user-modified")
	}
}

func TestSyncUpdatesUnchangedSkills(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "v1")
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}

	writeSkill(t, bundled, "alpha", "v2")
	report, err := SyncBundled(bundled, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %v", report.Updated)
	}
	if readSkill(t, runtime, "alpha") != "v2" {
		t.Fatal("unchanged skill was not refreshed")
	}
	m, _ := LoadManifest(runtime)
	if m.Skills["alpha"].UserModified {
		t.Fatal("skill updated upstream should not be user-modified")
	}
}

func TestSyncNeverStompsUserEdits(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "v1")
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}

	// User edits the skill locally.
	writeSkill(t, runtime, "alpha", "USER EDITS")
	writeSkill(t, bundled, "alpha", "v2-upstream")

	report, err := SyncBundled(bundled, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UserModified) != 1 {
		t.Fatalf("expected 1 user-modified, got %v", report.UserModified)
	}
	if got := readSkill(t, runtime, "alpha"); got != "USER EDITS" {
		t.Fatalf("user edit was stomped, got %q", got)
	}
	m, _ := LoadManifest(runtime)
	if !m.Skills["alpha"].UserModified {
		t.Fatal("edit not marked user-modified")
	}

	// A further resync still leaves it alone.
	report2, err := SyncBundled(bundled, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(report2.UserModified) != 1 {
		t.Fatalf("expected skip on resync, got %v", report2.UserModified)
	}
	if got := readSkill(t, runtime, "alpha"); got != "USER EDITS" {
		t.Fatalf("user edit was stomped on resync, got %q", got)
	}
}

func TestSyncPreservesHubSkills(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "A")
	writeSkill(t, runtime, "hubskill", "HUB") // not bundled

	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}
	if got := readSkill(t, runtime, "hubskill"); got != "HUB" {
		t.Fatal("hub skill was touched")
	}
	m, _ := LoadManifest(runtime)
	if _, ok := m.Skills["hubskill"]; ok {
		t.Fatal("hub skill should not be in bundled manifest")
	}
}

func TestSyncSameDirIsSafe(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "A")
	if _, err := SyncBundled(dir, dir); err != nil {
		t.Fatal(err)
	}
	if got := readSkill(t, dir, "alpha"); got != "A" {
		t.Fatal("same-dir sync corrupted the skill")
	}
}

func TestSyncReseedsDeletedUnmodifiedSkill(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "A")
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(runtime, "alpha")); err != nil {
		t.Fatal(err)
	}
	report, err := SyncBundled(bundled, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Seeded) != 1 {
		t.Fatalf("expected re-seed, got %v", report.Seeded)
	}
	if readSkill(t, runtime, "alpha") != "A" {
		t.Fatal("deleted unmodified skill not restored")
	}
}

func TestSyncDeletedModifiedSkillStaysGone(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "A")
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}
	// Mark as edited then delete.
	if err := MarkUserModified(runtime, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(runtime, "alpha")); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "alpha")); !os.IsNotExist(err) {
		t.Fatal("removed user-modified skill was resurrected")
	}
}

func TestManifestHelpers(t *testing.T) {
	bundled := t.TempDir()
	runtime := t.TempDir()
	writeSkill(t, bundled, "alpha", "A")
	if _, err := SyncBundled(bundled, runtime); err != nil {
		t.Fatal(err)
	}
	if !IsBundled(runtime, "alpha") || IsBundled(runtime, "nope") {
		t.Fatal("IsBundled wrong")
	}
	if IsUserModified(runtime, "alpha") {
		t.Fatal("should not be user-modified yet")
	}
	if err := MarkUserModified(runtime, "alpha"); err != nil {
		t.Fatal(err)
	}
	if !IsUserModified(runtime, "alpha") {
		t.Fatal("MarkUserModified did not stick")
	}
}