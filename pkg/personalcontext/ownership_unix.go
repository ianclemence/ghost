//go:build !windows

package personalcontext

import (
	"os"
	"syscall"
)

// keepOwner makes the file at dst belong to whoever owns the file it replaces.
// Ghost's service runs as root; an atomic rewrite through a new file would
// otherwise hand the owner's memory file to root.
func keepOwner(dst string, orig os.FileInfo) {
	if os.Geteuid() != 0 || orig == nil {
		return
	}
	if st, ok := orig.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(dst, int(st.Uid), int(st.Gid))
	}
}
