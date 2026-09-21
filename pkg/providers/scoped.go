package providers

import "strings"

// ScopedModels holds the session/owner view of which models are enabled for
// model cycling. A nil slice means "all enabled" (no filter); a non-nil slice
// is an explicit, ordered allow-list of option targets. The zero value is
// therefore the default: everything available is cycled.
type ScopedModels struct {
	ids []string
}

// AllEnabled reports whether no explicit filter is active.
func (s ScopedModels) AllEnabled() bool { return s.ids == nil }

// IDs returns a copy of the explicit ordered targets, or nil when all are on.
func (s ScopedModels) IDs() []string {
	if s.ids == nil {
		return nil
	}
	out := make([]string, len(s.ids))
	copy(out, s.ids)
	return out
}

// Set replaces the enabled set. A nil slice means all enabled.
func (s *ScopedModels) Set(ids []string) {
	if ids == nil {
		s.ids = nil
		return
	}
	cp := make([]string, len(ids))
	copy(cp, ids)
	s.ids = cp
}

// IsEnabled reports whether target is part of the active set.
func (s ScopedModels) IsEnabled(target string) bool {
	if s.ids == nil {
		return true
	}
	for _, x := range s.ids {
		if x == target {
			return true
		}
	}
	return false
}

// FilterScoped returns the options eligible for cycling given the scoped
// selection. When all are enabled (or the explicit list matches nothing in
// the input) the input is returned unchanged, so cycling never dead-ends.
func FilterScoped(options []ModelOption, sc ScopedModels) []ModelOption {
	if sc.AllEnabled() {
		return options
	}
	byTarget := map[string]ModelOption{}
	for _, o := range options {
		byTarget[o.Target] = o
		if o.Name != "" {
			byTarget[o.Name] = o
		}
	}
	out := make([]ModelOption, 0, len(sc.ids))
	seen := map[string]bool{}
	for _, id := range sc.ids {
		if o, ok := byTarget[id]; ok && !seen[o.Target] {
			seen[o.Target] = true
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return options
	}
	return out
}

// CycleScoped returns the next option after the current one within the scoped
// set, using the scoped ordering. ok is false when there is nothing to cycle
// to (fewer than two candidates).
func CycleScoped(options []ModelOption, sc ScopedModels, cur string, delta int) (ModelOption, bool) {
	eligible := FilterScoped(options, sc)
	if len(eligible) < 2 {
		return ModelOption{}, false
	}
	base := optionTargetBase(cur)
	idx := -1
	for i, o := range eligible {
		if o.Target == cur || o.Name == cur || optionTargetBase(o.Target) == base || optionTargetBase(o.Model) == base {
			idx = i
			break
		}
	}
	if idx == -1 {
		idx = 0
		// First press with no known current lands on the first candidate.
		if delta > 0 {
			return eligible[0], true
		}
		return eligible[len(eligible)-1], true
	}
	next := eligible[((idx+delta)%len(eligible)+len(eligible))%len(eligible)]
	return next, true
}

// TargetsFromOptions returns the targets (falling back to names) for a set of
// options, in order — the form persisted by the scoped-model selector.
func TargetsFromOptions(options []ModelOption) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		t := o.Target
		if t == "" {
			t = o.Name
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// NormalizeScoped collapses an explicit list that covers every option back to
// nil ("all enabled"). Options that no longer exist are dropped.
func NormalizeScoped(ids []string, options []ModelOption) []string {
	if ids == nil {
		return nil
	}
	known := map[string]bool{}
	for _, o := range options {
		known[o.Target] = true
		if o.Name != "" {
			known[o.Name] = true
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !known[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	all := TargetsFromOptions(options)
	if len(out) == len(all) {
		covered := true
		for _, t := range all {
			if !seen[t] {
				covered = false
				break
			}
		}
		if covered {
			return nil
		}
	}
	return out
}
