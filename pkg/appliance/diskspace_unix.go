//go:build linux || darwin

package appliance

import (
	"fmt"
	"syscall"
)

// statfsFn is a seam so tests can simulate a full disk without touching one.
var statfsFn = syscall.Statfs

// EnsureDiskSpace fails with a clear, actionable error when dir does not have
// at least needBytes free. Updates write a full binary before renaming, so a
// full disk used to surface as a silent failure or a truncated install; this
// turns it into a sentence a person can act on.
func EnsureDiskSpace(dir string, needBytes uint64) error {
	var st syscall.Statfs_t
	if err := statfsFn(dir, &st); err != nil {
		return nil // cannot determine: never block an update on a missing probe
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free < needBytes {
		return fmt.Errorf("not enough free disk space in %s: need about %d MB, %d MB available — free some space and run the update again",
			dir, needBytes>>20, free>>20)
	}
	return nil
}
