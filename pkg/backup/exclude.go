// Package backup centralizes the secrets boundary for every backup
// path. A backup must never accidentally contain plaintext provider
// secrets, OAuth tokens, or device credentials — restore then requires
// reconnecting integrations (documented product behavior), which is
// always safer than archiving credentials.
//
// All backup walkers (Web Console tar.gz, ghoststate export) must
// consult ShouldExclude. Rules are tested against a synthetic tree so
// new credential locations cannot slip in by convention alone.
package backup

import (
	"path/filepath"
	"strings"
)

// Reason classifies why a path is excluded.
type Reason string

const (
	ReasonSecret      Reason = "secret"
	ReasonCredential  Reason = "credential"
	ReasonTransient   Reason = "transient"
	ReasonNotExcluded Reason = ""
)

// excludedDirs are directory names (any depth) never archived.
var excludedDirs = map[string]Reason{
	".credentials": ReasonCredential, // OAuth refresh tokens, provider creds
	".calendar":    ReasonCredential, // gcalcli service-owned oauth/config
	"sessions":     ReasonTransient,  // runtime session state
	"state":        ReasonTransient,  // constantly-changing runtime state
	"journal":      ReasonTransient,  // excluded from console backups by policy
}

// excludedSuffixes are file-name suffixes never archived.
var excludedSuffixes = map[string]Reason{
	".secrets.json":       ReasonSecret,
	".gcalcli_oauth":      ReasonCredential,
	"calendar-token.json": ReasonCredential,
	".env":                ReasonSecret,
	".log":                ReasonTransient,
}

// excludedContains are path fragments (forward-slash) never archived.
var excludedContains = map[string]Reason{
	"gcalcli/oauth": ReasonCredential,
	".credentials/": ReasonCredential,
}

// ShouldExclude reports whether rel (slash-separated, root-relative) or
// base name must be skipped, and why. Empty reason means archive it.
func ShouldExclude(relPath string) (bool, Reason) {
	rel := filepath.ToSlash(relPath)
	base := filepath.Base(rel)
	for suffix, reason := range excludedSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true, reason
		}
	}
	for frag, reason := range excludedContains {
		if strings.Contains(rel, frag) {
			return true, reason
		}
	}
	for _, part := range strings.Split(rel, "/") {
		if reason, ok := excludedDirs[part]; ok {
			return true, reason
		}
	}
	return false, ReasonNotExcluded
}

// ShouldSkipDir reports whether a directory (base name) should be pruned
// from the walk entirely.
func ShouldSkipDir(base string) bool {
	_, ok := excludedDirs[base]
	return ok
}
