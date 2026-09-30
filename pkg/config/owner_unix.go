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

// MatchOwnerUnder gives path, and every folder between it and root, the owner
// of root. The daemon runs as root while the workspace belongs to the person
// who installed Ghost; a folder or file the daemon created privately inside it
// (screenshots, downloads) made every backup and update fail with "permission
// denied". root itself is never changed.
func MatchOwnerUnder(root, path string) {
	if os.Geteuid() != 0 || root == "" {
		return
	}
	st, err := os.Stat(root)
	if err != nil {
		return
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	root = filepath.Clean(root)
	for p := filepath.Clean(path); p != root && len(p) > len(root); p = filepath.Dir(p) {
		_ = os.Lchown(p, int(sys.Uid), int(sys.Gid))
	}
}

// RepairOwnership walks root and hands anything the daemon created privately
// (owned by someone other than root's owner) back to root's owner. It is a
// safety net behind MatchOwnerUnder: a backup or an update must never fail
// because one file was left readable only by the service account. It returns
// how many entries it changed. It does nothing unless running as root.
func RepairOwnership(root string) int {
	if os.Geteuid() != 0 || root == "" {
		return 0
	}
	st, err := os.Stat(root)
	if err != nil {
		return 0
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	fixed := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if s, ok := info.Sys().(*syscall.Stat_t); ok && (s.Uid != sys.Uid || s.Gid != sys.Gid) {
			if os.Lchown(p, int(sys.Uid), int(sys.Gid)) == nil {
				fixed++
			}
		}
		return nil
	})
	return fixed
}
