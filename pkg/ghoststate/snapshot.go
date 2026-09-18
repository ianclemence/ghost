package ghoststate

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/hardware"
)

// ExportMinFreeBytes is the pre-flight floor for writing an archive:
// refuse before producing a half-archive on a nearly-full disk.
const ExportMinFreeBytes = 256 << 20

// requireFreeSpace refuses when known-free space in dir falls below
// need. Unknown space never blocks (degrade gracefully, not falsely).
func requireFreeSpace(dir string, need uint64, what string) error {
	free, ok := hardware.DiskFreeBytes(dir)
	if !ok {
		return nil
	}
	if free < need {
		return fmt.Errorf("%s needs %d MB free, only %d MB available — free space or run `ghost state prune` to drop old snapshots", what, need>>20, free>>20)
	}
	return nil
}

// Automated snapshots make every mutating operation recoverable by
// construction: update snapshots before stopping services, and a
// scheduled backup keeps rolling recovery points. Same encrypted
// archive as manual export (reuse, not a second archiver); the
// passphrase is the existing vault master key, so no new secrets.

// SnapshotKeep is the rolling retention: newest N archives survive.
const SnapshotKeep = 5

// SnapshotPrefix names automated archives; UTC timestamps sort newest-last.
const SnapshotPrefix = "ghost-"

// BackupDir places snapshots beside the workspace's parent data dir:
// /var/lib/ghost/workspace → /var/lib/ghost/backups.
func BackupDir(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), "backups")
}

func snapshotName(t time.Time) string {
	return fmt.Sprintf("%s%s.gst", SnapshotPrefix, t.UTC().Format("20060102-150405"))
}

// SnapshotPassphrase derives the archive passphrase from the vault
// master key: recovery needs the key and nothing else.
func SnapshotPassphrase(configPath string) (string, error) {
	key, err := config.ResolveMasterKey(config.SecretsPath(configPath))
	if err != nil {
		return "", fmt.Errorf("snapshot key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// TakeSnapshot exports workspace+config (including secrets — a recovery
// point without credentials restores a lobotomized Ghost) to the
// backup dir. Fails closed: no archive, no mutation downstream.
func TakeSnapshot(workspace, configPath string) (string, error) {
	return TakeSnapshotAt(workspace, configPath, time.Now().UTC())
}

// TakeSnapshotAt is TakeSnapshot with an injectable clock (tests).
func TakeSnapshotAt(workspace, configPath string, at time.Time) (string, error) {
	pass, err := SnapshotPassphrase(configPath)
	if err != nil {
		return "", err
	}
	dir := BackupDir(workspace)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("snapshot dir: %w", err)
	}
	if err := requireFreeSpace(dir, ExportMinFreeBytes, "snapshot"); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, snapshotName(at))
	if _, err := Export(ExportOptions{
		Workspace: workspace, ConfigPath: configPath,
		Destination: dest, Passphrase: pass, IncludeSecrets: true,
	}); err != nil {
		return "", err
	}
	return dest, nil
}

// PruneSnapshots keeps the newest keep archives, deleting older ones.
// Returns removed paths, newest-last ordering for logs.
func PruneSnapshots(dir string, keep int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var snaps []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, SnapshotPrefix) && strings.HasSuffix(n, ".gst") {
			snaps = append(snaps, filepath.Join(dir, n))
		}
	}
	sort.Strings(snaps)
	var removed []string
	for len(snaps) > keep {
		oldest := snaps[0]
		snaps = snaps[1:]
		if err := os.Remove(oldest); err != nil {
			return removed, fmt.Errorf("prune %s: %w", oldest, err)
		}
		removed = append(removed, oldest)
	}
	return removed, nil
}

// SnapshotAndPrune takes a recovery snapshot then enforces retention.
func SnapshotAndPrune(workspace, configPath string, keep int) (string, []string, error) {
	dest, err := TakeSnapshot(workspace, configPath)
	if err != nil {
		return "", nil, err
	}
	removed, err := PruneSnapshots(BackupDir(workspace), keep)
	if err != nil {
		return dest, removed, err
	}
	return dest, removed, nil
}
