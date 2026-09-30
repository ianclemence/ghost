//go:build unix

package uploads

import (
	"os"
	"path/filepath"
	"syscall"
)

// matchOwner gives everything under the uploads root the same owner as the
// workspace itself. The daemon runs as root, but the workspace belongs to the
// install owner so the CLI can back it up and update; a root-only folder in
// the middle of it made every snapshot fail with "permission denied".
func matchOwner(workspace, path string) {
	if os.Geteuid() != 0 {
		return // not root: files already belong to us, nothing to fix
	}
	st, err := os.Stat(workspace)
	if err != nil {
		return
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	root := Dir(workspace)
	for p := path; ; p = filepath.Dir(p) {
		_ = os.Lchown(p, int(sys.Uid), int(sys.Gid))
		if p == root || len(p) <= len(workspace) {
			return
		}
	}
}
