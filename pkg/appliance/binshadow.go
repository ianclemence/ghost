package appliance

import (
	"path/filepath"
	"strings"
)

// Binary-shadow detection.
//
// A `ghost` binary earlier in PATH silently shadows a newer one installed
// later. This is not hypothetical here: `ghost update` installs to
// /usr/local/bin, while a user-local copy at ~/.local/bin usually comes FIRST
// in PATH. When the user-local copy is stale, `ghost` keeps running the old
// build after an update — the exact bug behind "I updated but still see the
// old behavior". This check makes the situation visible.

// BinaryPath is one `ghost` executable found on PATH, in PATH order.
type BinaryPath struct {
	// Path is the absolute path to the executable.
	Path string
	// Index is its position in PATH (0 = first, i.e. what wins).
	Index int
}

// FindGhostBinaries returns every `ghost` executable on PATH, in PATH order.
// findIn resolves an executable name within a directory (os/exec.LookPath-like
// but injectable for tests). Directories are de-duplicated; non-existing
// entries are skipped by findIn.
func FindGhostBinaries(pathEnv string, findIn func(dir, name string) (string, bool)) []BinaryPath {
	var out []BinaryPath
	seen := map[string]bool{}
	idx := 0
	for _, dir := range strings.Split(pathEnv, string(filepath.ListSeparator)) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		if p, ok := findIn(clean, "ghost"); ok {
			out = append(out, BinaryPath{Path: p, Index: idx})
			idx++
		}
	}
	return out
}

// BinaryShadow describes a stale earlier-PATH `ghost` that shadows a later one.
type BinaryShadow struct {
	// Wins is the path that actually runs (first in PATH).
	Wins string
	// Shadows is the newer binary that never runs.
	Shadows string
	// WinsVersion / ShadowsVersion are the versions observed (may be "").
	WinsVersion    string
	ShadowsVersion string
}

// DetectBinaryShadow reports whether the first `ghost` on PATH is STALE
// relative to another one later on PATH. version(path) returns the binary's
// version string ("" when unknown). Returns ok=false when there is no shadow
// to report: fewer than two binaries, or the winner is not older than the rest.
//
// The comparison is intentionally conservative: it only warns when the winner
// is a *different* version AND a later binary is different from it. Matching
// versions are not a shadow — they are harmless duplicates.
func DetectBinaryShadow(bins []BinaryPath, version func(path string) string) (BinaryShadow, bool) {
	if len(bins) < 2 {
		return BinaryShadow{}, false
	}
	winner := bins[0]
	wv := version(winner.Path)
	for _, b := range bins[1:] {
		bv := version(b.Path)
		if bv == "" || bv == wv {
			continue
		}
		return BinaryShadow{
			Wins:           winner.Path,
			Shadows:        b.Path,
			WinsVersion:    wv,
			ShadowsVersion: bv,
		}, true
	}
	return BinaryShadow{}, false
}
