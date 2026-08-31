package personalcontext

import (
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
