//go:build !windows

package browser

import (
	"os"
	"syscall"
)

// ownedByUser reports whether a /proc entry belongs to uid. Unknown counts as
// owned, matching the caller's "skip only what is clearly someone else's".
func ownedByUser(st os.FileInfo, uid int) bool {
	sys, ok := st.Sys().(*syscall.Stat_t)
	return !ok || int(sys.Uid) == uid
}

func killProcess(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
