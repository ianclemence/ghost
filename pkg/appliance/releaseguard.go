package appliance

import "strings"

// Release guard: production must run a release, not a working tree.
//
// `ghost update` builds and deploys whatever is in the checkout. If the
// checkout has uncommitted changes, that would push prototype work straight
// into the running install. This guard blocks it unless --force is given.

// DirtyReason explains why a checkout is not releasable. Empty reason means
// clean.
type DirtyReason struct {
	// Modified/untracked paths (bounded).
	Paths []string
	// Detached describes a checkout that is not on a branch (e.g. a tag),
	// which is fine for releases; Branch is provided for the message.
	Branch string
}

// IsClean reports whether the parsed `git status --porcelain` output
// represents a clean tree. Untracked files count as dirty for release
// purposes (they could be prototype artifacts that get built).
func IsClean(porcelain string) bool {
	return strings.TrimSpace(porcelain) == ""
}

// SummarizeDirty turns porcelain output into a bounded list of paths for the
// block message.
func SummarizeDirty(porcelain string, max int) []string {
	if max <= 0 {
		max = 5
	}
	var out []string
	for _, line := range strings.Split(porcelain, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Porcelain format: XY<space>path
		path := line
		if len(line) > 3 {
			path = strings.TrimSpace(line[3:])
		}
		out = append(out, path)
		if len(out) >= max {
			break
		}
	}
	return out
}

// scratch
