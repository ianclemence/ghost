//go:build linux || darwin

package hardware

import "syscall"

// freeBytes returns available bytes for the filesystem holding path,
// false when unknown. Callers degrade gracefully on unknown: never block
// on a failed stat.
func freeBytes(path string) (uint64, bool) {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}

// diskInfoGB returns (freeGB, totalGB, freePct) for the filesystem holding
// path. It returns zeros on error so the caller degrades to normal.
func diskInfoGB(path string) (int, int, int) {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, 0, 0
	}
	bs := int64(st.Bsize)
	total := int64(st.Blocks) * bs
	free := int64(st.Bavail) * bs
	const gb = 1024 * 1024 * 1024
	totalGB := int(total / gb)
	freeGB := int(free / gb)
	pct := int(free * 100 / total)
	return freeGB, totalGB, pct
}
