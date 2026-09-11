package rag

import "testing"

func TestTrustForSource(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		// Model-written memory is inference, never authority.
		{"memory_tool", "inferred"},
		{"memory_tool@context:work", "inferred"},
		{"journal", "inferred"},
		{"curate", "inferred"},
		// Network-derived content is untrusted until user-confirmed.
		{"memory_tool+web", "untrusted"},
		{"memory_tool+web@context:work", "untrusted"},
		{"web_fetch", "untrusted"},
		{"scraper", "untrusted"},
		// Anything else stays conservative.
		{"user_notes", "unknown"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := trustForSource(c.source); got != c.want {
			t.Errorf("trustForSource(%q) = %q, want %q", c.source, got, c.want)
		}
	}
}
