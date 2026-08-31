package personalcontext

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Compact is a conservative, background consolidation of a Personal Context
// store. It is intentionally minimal and safe: it NEVER creates a new fact,
// NEVER merges two distinct values, and NEVER guesses that one belief replaces
// another (a coarse predicate like "preference/prefers" can legitimately hold
// multiple distinct values — coffee and tea can both be things someone likes).
//
// It only removes definitive redundancy:
//
//  1. Exact duplicates — two or more current entries with the same
//     (subject, predicate, value). It keeps the most recently stated entry and
//     rejects the older ones, so provenance is never lost.
//
// The kept entry retains its sources and timestamps (provenance). Returns the
// number of entries rejected.
func Compact(store *Store) (int, error) {
	if store == nil {
		return 0, nil
	}

	cur := store.Current()
	byKey := map[string][]Entry{} // "subject|predicate|value" -> entries
	for _, e := range cur {
		k := e.Subject + "|" + e.Predicate + "|" + entryValueString(e)
		byKey[k] = append(byKey[k], e)
	}

	rejected := 0
	// Sort keys so the outcome is deterministic (map iteration order is not).
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sortStrings(keys)

	for _, k := range keys {
		group := byKey[k]
		if len(group) < 2 {
			continue
		}
		// Keep the most recently updated entry.
		keep := group[0]
		for _, e := range group[1:] {
			if e.UpdatedAt.After(keep.UpdatedAt) {
				keep = e
			}
		}
		for _, e := range group {
			if e.ID == keep.ID {
				continue
			}
			if err := store.Forget(e.ID); err != nil {
				return rejected, err
			}
			rejected++
		}
	}

	return rejected, nil
}

// sortStrings sorts a string slice ascending (ASCII).
func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// preferencePredicates lists the additive preference predicates that Ghost
// treats as "a belief that can accumulate" (multiple distinct values are valid).
// Used to keep Compact and any consumer aware of which predicates are additive.
func preferencePredicates() []string {
	return []string{
		"preference/likes",
		"preference/prefers",
		"preference/favorite",
		"preference/communication.style",
	}
}

// isAdditivePreference reports whether a predicate is an additive preference.
func isAdditivePreference(predicate string) bool {
	for _, p := range preferencePredicates() {
		if p == predicate {
			return true
		}
	}
	return strings.Contains(predicate, "likes") || strings.Contains(predicate, "prefer") || strings.Contains(predicate, "favorite")
}

// durableDirs tracks the workspace-relative subdirectory layout for the
// curated profile materialized from Personal Context.
const curatedProfileDir = "knowledge/self"

// curlCuratedKnownKinds are the kinds that are safe to materialize into the
// human-readable curated profile (durable, identity/belief data — not
// transient facts).
var curatedKnownKinds = map[Kind]bool{
	KindIdentity:     true,
	KindPreference:   true,
	KindFact:         true,
	KindGoal:         true,
	KindRelationship: true,
	KindRoutine:      true,
}

// MaterializeCuratedProfile writes the current durable Personal Context facts
// into the curated user-profile.md (the always-injected profile layer) so the
// curated profile is actually populated and stays in sync with Ghost's
// structured memory. It never invents facts — it only mirrors what Ghost
// already believes, and it degrades to a no-op if there is nothing durable or
// the workspace is unavailable. It returns the number of facts written.
func MaterializeCuratedProfile(workspace string, store *Store) (int, error) {
	if store == nil {
		return 0, nil
	}
	cur := store.Current()
	lines := make([]string, 0, len(cur))
	for _, e := range cur {
		if !curatedKnownKinds[e.Kind] {
			continue
		}
		val := Value(e)
		if val == "" {
			continue
		}
		label := Label(e.Predicate)
		if label == "" {
			label = e.Predicate
		}
		line := label + ": " + val
		if e.ReinforceCount > 0 {
			line += " (reinforced " + strconv.Itoa(e.ReinforceCount) + "x)"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return 0, nil
	}

	path := filepath.Join(workspace, curatedProfileDir, "user-profile.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, err
	}
	content := strings.Join(lines, "\n§\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return 0, err
	}
	return len(lines), nil
}
