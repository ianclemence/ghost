//go:build linux || darwin

package golden

import "syscall"

func diskFreeMB(path string) int {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0
	}
	return int(uint64(st.Bavail) * uint64(st.Bsize) >> 20)
}
