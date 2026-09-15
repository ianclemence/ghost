package appliance

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/ghoststate"
)

// PreUpdateSnapshot takes a recovery snapshot of the appliance
// workspace before any mutating update step. It is the Snapshot stage
// of RunUpdate: failure aborts the update while services still run.
// Secrets are included — a recovery point without credentials restores
// a lobotomized appliance — sealed under the existing vault key.
func PreUpdateSnapshot() error {
	workspace := os.Getenv("GHOST_WORKSPACE_DIR")
	if workspace == "" {
		workspace = DefaultWorkspaceDir
	}
	configPath := filepath.Join(DefaultGhostDir, "config", "config.json")
	dest, removed, err := ghoststate.SnapshotAndPrune(workspace, configPath, ghoststate.SnapshotKeep)
	if err != nil {
		return err
	}
	fmt.Printf("  Recovery snapshot: %s\n", dest)
	for _, r := range removed {
		fmt.Printf("  Pruned old snapshot: %s\n", r)
	}
	return nil
}
