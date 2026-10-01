//go:build !windows

package localtrust

import (
	"os"
	"syscall"
)

// matchOwner gives the token file the same owner as its directory when the
// gateway runs as root. A system install runs the gateway as root while the
// owner's own CLI and terminal run as the owner; a root-only token would lock
// them out of the very routes it exists to protect them on.
func matchOwner(dir, path string) {
	if os.Geteuid() != 0 {
		return
	}
	st, err := os.Stat(dir)
	if err != nil {
		return
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(path, int(sys.Uid), int(sys.Gid))
	}
}
