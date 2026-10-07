package config

import "testing"

// The embedding default is a constant every reference follows; legacy
// values (nomic era) and blanks resolve to it so old configs keep
// working and the index gets re-embedded instead of breaking.
func TestEmbeddingModelOrDefault(t *testing.T) {
	cases := map[string]string{
		"":                        DefaultEmbeddingTag,
		"nomic-embed-text":        DefaultEmbeddingTag,
		"nomic-embed-text:latest": DefaultEmbeddingTag,
		"embeddinggemma":          "embeddinggemma",
		"embeddinggemma:latest":   "embeddinggemma:latest",
		"embeddinggemma-2":        "embeddinggemma-2",
		"custom-embed":            "custom-embed",
	}
	for in, want := range cases {
		cfg := DefaultConfig()
		cfg.Agents.Defaults.EmbeddingModel = in
		if got := cfg.EmbeddingModelOrDefault(); got != want {
			t.Errorf("EmbeddingModelOrDefault(%q) = %q, want %q", in, got, want)
		}
	}
}
