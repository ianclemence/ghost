package tools

import "strings"

// Source facts: what a read-only information tool actually obtained, recorded
// in structured form instead of in prose.
//
// The runtime needs to know whether a page was read through, whether a result
// was a listing or a snippet, and which publications stand behind an answer —
// for audit, replay, activity and evaluation. The owner generally does not.
// Recording the facts here, rather than narrating them in the tool's text, is
// what lets the limitation policy decide separately from the retrieval.
const (
	// SourceAccessFullText: the page's own content was read.
	SourceAccessFullText = "full_text"
	// SourceAccessPartial: content was read but truncated.
	SourceAccessPartial = "partial_text"
	// SourceAccessListings: only search listings — titles, URLs, snippets.
	SourceAccessListings = "search_listings"
	// SourceAccessFailed: the requested resource could not be read.
	SourceAccessFailed = "failed"
)

// Evidence keys used for source facts. Stable, so events and activity can read
// them without guessing.
const (
	EvidenceKeySummary      = "summary"
	EvidenceKeySources      = "sources"
	EvidenceKeySourceAccess = "source_access"
	EvidenceKeyRequested    = "requested_source"
)

// sourceFactsCap bounds every source list so an event payload can never grow
// without limit.
const sourceFactsCap = 8

// DescribeSources records what a read-only tool obtained: a bounded one-line
// summary of the action, the sources behind it, and how they were accessed.
// It merges into an existing evidence map so a tool can attach both proof of a
// state change and the shape of its evidence.
func DescribeSources(ev map[string]interface{}, summary string, sources []string, access string) map[string]interface{} {
	if ev == nil {
		ev = map[string]interface{}{}
	}
	if s := strings.TrimSpace(summary); s != "" {
		ev[EvidenceKeySummary] = truncateRunes(s, 160)
	}
	if len(sources) > 0 {
		seen := map[string]bool{}
		clean := make([]string, 0, len(sources))
		for _, s := range sources {
			s = strings.TrimSpace(s)
			if s == "" || seen[strings.ToLower(s)] {
				continue
			}
			seen[strings.ToLower(s)] = true
			clean = append(clean, truncateRunes(s, 80))
			if len(clean) >= sourceFactsCap {
				break
			}
		}
		if len(clean) > 0 {
			ev[EvidenceKeySources] = clean
		}
	}
	if access != "" {
		ev[EvidenceKeySourceAccess] = access
	}
	return ev
}

// EvidenceSummary returns a tool-declared one-line description of what it did,
// or "" when the tool did not declare one.
func EvidenceSummary(res *ToolResult) string {
	if res == nil || res.Evidence == nil {
		return ""
	}
	s, _ := res.Evidence[EvidenceKeySummary].(string)
	return strings.TrimSpace(s)
}

// EvidenceSources returns the sources behind a tool's result, bounded.
func EvidenceSources(res *ToolResult) []string {
	if res == nil || res.Evidence == nil {
		return nil
	}
	switch v := res.Evidence[EvidenceKeySources].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

// EvidenceSourceAccess returns the access level a tool recorded, or "".
func EvidenceSourceAccess(res *ToolResult) string {
	if res == nil || res.Evidence == nil {
		return ""
	}
	s, _ := res.Evidence[EvidenceKeySourceAccess].(string)
	return strings.TrimSpace(s)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
