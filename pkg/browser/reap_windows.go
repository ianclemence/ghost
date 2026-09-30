package browser

import (
	"errors"
	"os"
	"syscall"
)

// Windows has no /proc, so there are never orphans to find; these exist so the
// package builds.
func ownedByUser(os.FileInfo, int) bool { return true }

func killProcess(int, syscall.Signal) error { return errors.New("not supported on windows") }
