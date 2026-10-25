//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"syscall"
)

// inheritOwner gives a folder the service just created the owner of the folder
// it sits in. Ghost's service runs as root, so without this everything it makes
// inside the owner's workspace (a project folder, say) belongs to root and the
// owner cannot edit their own project. It does nothing when not root.
func inheritOwner(path string) {
	if os.Geteuid() != 0 {
		return
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(path, int(st.Uid), int(st.Gid))
	}
}
