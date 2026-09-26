//go:build !linux

package tools

// readSystemStatus is a stub on non-Linux platforms: nothing is faked,
// everything is honestly reported as unreadable.
func readSystemStatus() systemStatus {
	return systemStatus{
		Unsupported: []string{"cpu temperature", "memory", "load average", "uptime", "disk space"},
	}
}
