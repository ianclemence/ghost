//go:build !linux

package tools

// bwrapAvailable is always false off Linux; isolation falls back to env
// sanitization only (auto) or refuses (require).
func bwrapAvailable() bool { return false }

func bwrapArgs(cwd, workspace string, allowNetwork bool) []string { return nil }
