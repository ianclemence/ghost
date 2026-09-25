//go:build !linux && !darwin

package appliance

// EnsureDiskSpace is a no-op where a statfs probe is unavailable.
func EnsureDiskSpace(dir string, needBytes uint64) error { return nil }
