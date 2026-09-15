package ghoststate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
)

// Snapshots reuse the vault master key: no ambient key may leak in, and
// the generated legacy key file must resolve identically twice.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Setenv("GHOST_MASTER_KEY", "")
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	cfgPath := filepath.Join(cfgDir, "config.json")

	dest, err := TakeSnapshot(ws, cfgPath)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	base := filepath.Base(dest)
	if !strings.HasPrefix(base, "ghost-") || !strings.HasSuffix(base, ".gst") {
		t.Fatalf("snapshot name %q must be ghost-<utc>.gst", base)
	}
	if filepath.Dir(dest) != BackupDir(ws) {
		t.Fatalf("snapshot must land in BackupDir, got %s", dest)
	}
	// The same passphrase must decrypt what was written: derive it the
	// same way and import into a fresh target.
	pass, err := SnapshotPassphrase(cfgPath)
	if err != nil {
		t.Fatalf("SnapshotPassphrase: %v", err)
	}
	targetWS := t.TempDir()
	targetCfgDir := t.TempDir()
	imported, err := Import(ImportOptions{
		Workspace: targetWS, ConfigPath: filepath.Join(targetCfgDir, "config.json"),
		Source: dest, Passphrase: pass,
	})
	if err != nil {
		t.Fatalf("Import snapshot: %v", err)
	}
	if imported.GhostID == "" {
		t.Fatal("imported snapshot must carry identity")
	}
	d, err := db.NewDB(targetWS)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var msgCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount); err != nil || msgCount != 1 {
		t.Fatalf("messages: got %d, err %v", msgCount, err)
	}
}

// Retention keeps the newest N archives and prunes the rest.
func TestPruneSnapshots(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		name := snapshotName(base.Add(time.Duration(i) * time.Hour))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A non-snapshot file must survive pruning.
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	removed, err := PruneSnapshots(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("6 snapshots keep 5 must remove 1, removed %d", len(removed))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 { // 5 snapshots + keep.txt
		t.Fatalf("want 5 snapshots + keep.txt, got %d files", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal("non-snapshot files must survive pruning")
	}
	// Missing dir is a no-op, never an error.
	if _, err := PruneSnapshots(filepath.Join(dir, "absent"), 5); err != nil {
		t.Fatalf("absent dir must be a no-op: %v", err)
	}
}

func TestRequireFreeSpace(t *testing.T) {
	dir := t.TempDir()
	// Absurd need always fails with the prune remedy.
	if err := requireFreeSpace(dir, 1<<62, "snapshot"); err == nil {
		t.Fatal("impossible need must fail")
	} else if !strings.Contains(err.Error(), "ghost state prune") {
		t.Fatalf("must name the remedy: %v", err)
	}
	// Trivial need passes on any real filesystem.
	if err := requireFreeSpace(dir, 1, "snapshot"); err != nil {
		t.Fatalf("trivial need must pass: %v", err)
	}
}
