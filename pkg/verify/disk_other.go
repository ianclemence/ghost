//go:build !linux && !darwin

package verify

import "errors"

func diskUsedPercent(path string) (int, error) {
	return 0, errors.New("disk info unsupported on this platform")
}
