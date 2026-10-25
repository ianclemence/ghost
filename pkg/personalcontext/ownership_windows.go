//go:build windows

package personalcontext

import "os"

func keepOwner(string, os.FileInfo) {}
