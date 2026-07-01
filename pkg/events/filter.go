package events

// EventFilter determines whether an event matches a subscription.
type EventFilter interface {
	Matches(evt Event) bool
}

// FilterFunc is a convenience type for implementing EventFilter as a function.
type FilterFunc func(evt Event) bool

// Matches implements EventFilter.
func (f FilterFunc) Matches(evt Event) bool {
	return f(evt)
}

// OfKindFilter matches events of specific kinds.
type OfKindFilter struct {
	kinds map[EventKind]bool
}

// OfKind creates a filter matching events of the given kinds.
func OfKind(kinds ...EventKind) *OfKindFilter {
	m := make(map[EventKind]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return &OfKindFilter{kinds: m}
}

// Matches implements EventFilter.
func (f *OfKindFilter) Matches(evt Event) bool {
	return f.kinds[evt.Kind]
}

// KindPrefixFilter matches events whose kind starts with a prefix.
type KindPrefixFilter struct {
	prefix EventKind
}

// KindPrefix creates a filter matching events with the given kind prefix.
func KindPrefix(prefix EventKind) *KindPrefixFilter {
	return &KindPrefixFilter{prefix: prefix}
}

// Matches implements EventFilter.
func (f *KindPrefixFilter) Matches(evt Event) bool {
	return kindMatches(evt.Kind, f.prefix)
}

// SourceFilter matches events from a specific source.
type SourceFilter struct {
	source string
}

// Source creates a filter matching events from the given source.
func Source(source string) *SourceFilter {
	return &SourceFilter{source: source}
}

// Matches implements EventFilter.
func (f *SourceFilter) Matches(evt Event) bool {
	return evt.Source == f.source
}

// ScopeFilter matches events with specific scope values.
type ScopeFilter struct {
	values map[string]string
}

// Scope creates a filter matching events with the given scope key-value pairs.
func Scope(values map[string]string) *ScopeFilter {
	return &ScopeFilter{values: values}
}

// Matches implements EventFilter.
func (f *ScopeFilter) Matches(evt Event) bool {
	if evt.Scope == nil {
		return false
	}
	for k, v := range f.values {
		if evt.Scope[k] != v {
			return false
		}
	}
	return true
}

// AndFilter combines multiple filters with AND logic.
type AndFilter struct {
	filters []EventFilter
}

// And combines filters with AND logic (all must match).
func And(filters ...EventFilter) *AndFilter {
	return &AndFilter{filters: filters}
}

// Matches implements EventFilter.
func (f *AndFilter) Matches(evt Event) bool {
	for _, filter := range f.filters {
		if !filter.Matches(evt) {
			return false
		}
	}
	return true
}

// OrFilter combines multiple filters with OR logic (any must match).
type OrFilter struct {
	filters []EventFilter
}

// Or combines filters with OR logic (any must match).
func Or(filters ...EventFilter) *OrFilter {
	return &OrFilter{filters: filters}
}

// Matches implements EventFilter.
func (f *OrFilter) Matches(evt Event) bool {
	for _, filter := range f.filters {
		if filter.Matches(evt) {
			return true
		}
	}
	return false
}

// NotFilter inverts a filter.
type NotFilter struct {
	filter EventFilter
}

// Not inverts a filter.
func Not(filter EventFilter) *NotFilter {
	return &NotFilter{filter: filter}
}

// Matches implements EventFilter.
func (f *NotFilter) Matches(evt Event) bool {
	return !f.filter.Matches(evt)
}

// AlwaysMatch is a filter that matches all events.
var AlwaysMatch = FilterFunc(func(evt Event) bool { return true })

// NeverMatch is a filter that matches no events.
var NeverMatch = FilterFunc(func(evt Event) bool { return false })
