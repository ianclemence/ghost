//go:build windows

package config

// KeepOwner is not needed where files are not owned by a separate service user.
func KeepOwner(tmpPath, target string) {}
