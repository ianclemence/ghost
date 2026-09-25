// Package browser executable discovery.
//
// Why this exists: the agent-browser CLI launches a Chromium-compatible
// browser, and on some Linux distributions the *system* Chromium crashes
// immediately under --remote-debugging-port (observed: SIGTRAP inside
// Chromium's network-sandbox namespace code on Debian trixie / arm64).
// Playwright's bundled headless_shell does not. The failure is silent —
// Chrome "exits early without writing DevToolsActivePort" — so a browser
// action just hangs until the caller's timeout.
//
// Rather than hardcode a machine path into the product, Ghost discovers a
// usable executable deterministically and exports it to agent-browser via
// AGENT_BROWSER_EXECUTABLE_PATH. An operator-set value always wins.
package browser

import (
	"os"
	"path/filepath"
	"sort"
)

// ProfileEnv is the environment variable agent-browser honors for a persistent
// Chrome profile directory. Ghost points it at the session's context-isolated
// profile so cookies and logins survive restarts; an operator-set value wins.
const ProfileEnv = "AGENT_BROWSER_PROFILE"

// ExecutableEnv is the environment variable agent-browser honors for a
// custom browser binary. Ghost uses it to steer agent-browser away from a
// broken system Chromium without patching the CLI.
const ExecutableEnv = "AGENT_BROWSER_EXECUTABLE_PATH"

// PlaywrightCacheDirEnv / PlaywrightBrowsersPathEnv are the standard
// Playwright cache locations. Honoring them means discovery follows the
// same install the tests and dev tooling use.
const (
	playwrightCacheEnv = "PLAYWRIGHT_BROWSERS_PATH"
	homeCacheRel       = ".cache/ms-playwright"
)

// Candidate is one browser executable discovery considered.
type Candidate struct {
	Path  string // absolute path to the executable
	Rank  int    // lower is preferred
	Label string // human label for diagnostics
}

// DiscoverExecutable returns the path of a browser executable to hand to
// agent-browser, or "" when discovery should not override agent-browser's
// own auto-detection.
//
// Precedence:
//  1. An operator-set AGENT_BROWSER_EXECUTABLE_PATH (we return "" so the
//     caller leaves it untouched — the operator's choice is authoritative).
//  2. Playwright headless_shell binaries, newest first (these are built for
//     headless automation and avoid the system-Chromium sandbox crash).
//
// We deliberately do NOT return a system chrome path: agent-browser already
// auto-detects those, and overriding them with a path it would have found
// anyway only risks surprising the operator.
//
// roots lets tests inject a filesystem root; production passes nil and the
// real home/cache directories are searched.
func DiscoverExecutable(roots []string) string {
	// Operator override is authoritative: signal "leave it alone".
	if v := os.Getenv(ExecutableEnv); v != "" {
		return ""
	}

	searchRoots := roots
	if searchRoots == nil {
		searchRoots = playwrightRoots()
	}
	cands := findPlaywrightHeadlessShells(searchRoots)
	if len(cands) == 0 {
		return ""
	}
	// Stable ordering already applied; return the best.
	return cands[0].Path
}

// playwrightRoots lists the directories that may contain a Playwright
// browser cache, most specific first.
func playwrightRoots() []string {
	var roots []string
	if p := os.Getenv(playwrightCacheEnv); p != "" {
		roots = append(roots, p)
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		roots = append(roots, filepath.Join(home, homeCacheRel))
	}
	return roots
}

// findPlaywrightHeadlessShells scans roots for
// <root>/chromium_headless_shell-<build>/chrome-linux/headless_shell and
// returns them newest build first. Version parsing is intentionally simple:
// the numeric suffix is compared as a string-sorted key, which is correct
// for the zero-padded integer builds Playwright emits.
func findPlaywrightHeadlessShells(roots []string) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		var dirs []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if filepathHasPrefix(e.Name(), "chromium_headless_shell-") {
				dirs = append(dirs, e.Name())
			}
		}
		// Newest build first: reverse lexical order of the suffixed dirs.
		sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
		for _, d := range dirs {
			exe := filepath.Join(root, d, "chrome-linux", "headless_shell")
			if seen[exe] {
				continue
			}
			info, err := os.Stat(exe)
			if err != nil || info.IsDir() {
				continue
			}
			if info.Mode()&0o111 == 0 {
				continue // not executable
			}
			seen[exe] = true
			out = append(out, Candidate{Path: exe, Rank: 0, Label: d})
		}
	}
	return out
}

func filepathHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
