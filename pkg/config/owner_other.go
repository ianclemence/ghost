//go:build windows

package config

// KeepOwner is not needed where files are not owned by a separate service user.
func KeepOwner(tmpPath, target string) {}

// MatchOwnerUnder is not needed where files are not owned by a separate service user.
func MatchOwnerUnder(root, path string) {}

// RepairOwnership is not needed where files are not owned by a separate service user.
func RepairOwnership(root string) int { return 0 }
