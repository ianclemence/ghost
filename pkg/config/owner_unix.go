//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"syscall"
)

// KeepOwner gives a freshly written file the owner of the file it is about to
// replace. Ghost's services run as root while the person who installed it owns
// the files and runs the terminal as themselves. A save from the daemon or the
// console used to hand the new file to root, and from then on the owner's own
// terminal could not read its settings and would not start ("permission
// denied"). When the target does not exist yet, the folder's owner is used.
// It does nothing unless running as root, and never fails the write.
func KeepOwner(tmpPath, target string) {
	if os.Geteuid() != 0 {
		return
	}
	st, err := os.Stat(target)
	if err != nil {
		st, err = os.Stat(filepath.Dir(target))
		if err != nil {
			return
		}
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(tmpPath, int(sys.Uid), int(sys.Gid))
	}
}
