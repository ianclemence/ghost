//go:build !linux && !darwin

package hardware

// diskInfoGB is a no-op on platforms without statfs; callers degrade to
// "normal" rather than reporting a false alarm.
func diskInfoGB(path string) (int, int, int) { return 0, 0, 0 }
