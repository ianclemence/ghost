package ghoststate

import (
	"os"
	"path/filepath"
	"testing"
)

// A live workspace collects user files (screenshots, notes) and occasional
// folders Ghost does not own (manual backups, exports). A single unrecognized
// path must never abort the snapshot — that silently ended backups on a real
// device. Root-level files travel as user content; other unknown paths are
// recorded as skipped.
func TestExportToleratesUserContentAndUnknownFolders(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)

	// Real state stays real state.
	if err := os.MkdirAll(filepath.Join(ws, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "memory", "2026-09-25.md"), []byte("a note"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A user file dropped at the workspace root (this is what broke backups).
	if err := os.WriteFile(filepath.Join(ws, "chelsea-en.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A directory Ghost does not own (e.g. a hand-made skills backup).
	backupDir := filepath.Join(ws, ".skills-backup-20260925", "calendar")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "SKILL.md.disabled"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	manifest, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("a stray file must not abort the snapshot: %v", err)
	}

	if manifest.File("chelsea-en.png") == nil {
		t.Error("a root-level user file should travel in the snapshot")
	}
	if manifest.File("memory/2026-09-25.md") == nil {
		t.Error("real state must still be captured")
	}
	skipped := false
	for _, p := range manifest.Skipped {
		if p == ".skills-backup-20260925" || p == ".skills-backup-20260925/calendar/SKILL.md.disabled" {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("unknown folder should be recorded as skipped, got %v", manifest.Skipped)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive was not written: %v", err)
	}
}
