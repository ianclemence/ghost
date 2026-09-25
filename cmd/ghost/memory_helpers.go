package main

import "github.com/ianclemence/ghost/pkg/personalcontext"

// firstSourceRef returns the first provenance ref ("session:message") for a
// belief, or "" when none was recorded. Used by the owner-facing memory view
// so the receipt can point at where a fact came from.
func firstSourceRef(sources []personalcontext.Source) string {
	for _, s := range sources {
		if ref := s.Ref; ref != "" {
			return ref
		}
	}
	return ""
}

// derefString safely unwraps an optional string pointer for JSON views.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
