package hardware

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// A Pod loses power. MarkRunning leaves a marker while the daemon is up and
// reports whether the previous run left one behind, which means it did not stop
// cleanly: a power cut, a crash or a pulled plug. MarkStopped removes it on a
// clean stop. Nothing is lost either way (the database is journaled and
// integrity-checked on every start); the marker exists so the owner is told.
func MarkRunning(path string) (unclean bool) {
	if _, err := os.Stat(path); err == nil {
		unclean = true
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
	return unclean
}

// MarkStopped records a clean stop.
func MarkStopped(path string) { _ = os.Remove(path) }
