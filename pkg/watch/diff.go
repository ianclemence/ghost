package watch

import "sort"

// Change is one deterministic difference between the baseline state and the
// state a probe just observed. It is computed by comparing two maps of
// strings — no model is ever asked whether something changed.
type Change struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Fingerprint is the stable identity of one delivered notice:
// watch:<id>:<field>:<from>→<to>. A reversal (gate A→B then B→A) is a
// different fingerprint from the original, so it notifies again — while an
// identical repeat never does.
func (c Change) Fingerprint(watchID string) string {
	return "watch:" + watchID + ":" + c.Field + ":" + c.From + "→" + c.To
}

// Metadata reports whether a field is probe bookkeeping rather than the
// state of the thing being watched. Metadata keys are underscore-prefixed by
// convention (the sandbox and the flight probe both honor it) and are never
// diffed and never notified.
func Metadata(field string) bool {
	return len(field) > 0 && field[0] == '_'
}

// Diff compares the observed state against the baseline and returns the
// meaningful changes in deterministic (alphabetical) order. Determinism
// matters: the same two states must always produce the same notices in the
// same words, so two runs over the same evidence agree.
func Diff(baseline, current map[string]string) []Change {
	if len(baseline) == 0 || len(current) == 0 {
		return nil
	}
	keys := map[string]struct{}{}
	for k := range baseline {
		keys[k] = struct{}{}
	}
	for k := range current {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		if Metadata(k) {
			continue
		}
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var out []Change
	for _, k := range sorted {
		from, hadBefore := baseline[k]
		to, hasNow := current[k]
		if !hadBefore {
			// A field that only exists in the current state has no prior
			// value to move from; its appearance is not a change.
			continue
		}
		if trim(from) == trim(to) {
			continue
		}
		if !hasNow {
			to = ""
		}
		out = append(out, Change{Field: k, From: trim(from), To: trim(to)})
	}
	return out
}

// DiffStates compares two arbitrary states (used when the baseline has not
// been captured yet but a previous current state exists).
func DiffStates(previous, current map[string]string) []Change {
	return Diff(previous, current)
}

func trim(s string) string {
	// Whitespace-only differences are not changes in the world.
	out := []rune{}
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			space = len(out) > 0
			continue
		}
		if space {
			out = append(out, ' ')
			space = false
		}
		out = append(out, r)
	}
	return string(out)
}
