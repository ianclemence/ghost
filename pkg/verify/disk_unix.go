//go:build linux || darwin

package verify

import "syscall"

func diskUsedPercent(path string) (int, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 0, syscall.EINVAL
	}
	used := st.Blocks - st.Bfree
	return int(used * 100 / st.Blocks), nil
}
