package providers

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestAnthropicThinkingGenerationRouting verifies the opt-in thinking path
// uses adaptive mode on 4.6+/5.x models (where enabled+budget is deprecated
// or rejected with 400) and the legacy budget form only on older models.
// Default (no thinking_level) sends no thinking param at all.
func TestAnthropicThinkingGenerationRouting(t *testing.T) {
	cases := []struct {
		model    string
		adaptive bool
	}{
		{"claude-sonnet-5", true},
		{"claude-opus-5", true},
		{"claude-fable-5", true},
		{"claude-opus-4-7", true},
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-5", false},
		{"claude-sonnet-4-5", false},
		{"claude-haiku-4-5", false},
		{"claude-3-5-sonnet", false},
		{"mystery-model", true}, // unknown: forward-looking default
	}
	for _, tc := range cases {
		params := anthropic.MessageNewParams{MaxTokens: 16000}
		applyThinkingConfig(&params, tc.model, "medium")
		if tc.adaptive && params.Thinking.OfAdaptive == nil {
			t.Errorf("%s: expected adaptive thinking", tc.model)
		}
		if !tc.adaptive && params.Thinking.OfEnabled == nil {
			t.Errorf("%s: expected legacy enabled+budget thinking", tc.model)
		}
	}
}

func TestAnthropicAdaptiveEffortMapping(t *testing.T) {
	cases := []struct {
		level string
		want  anthropic.OutputConfigEffort
	}{
		{"low", anthropic.OutputConfigEffortLow},
		{"medium", anthropic.OutputConfigEffortMedium},
		{"high", anthropic.OutputConfigEffortHigh},
		{"max", anthropic.OutputConfigEffortMax},
		{"anything-else", anthropic.OutputConfigEffortHigh},
	}
	for _, tc := range cases {
		if got := anthropicAdaptiveEffort(tc.level); got != tc.want {
			t.Errorf("level %q: got %q want %q", tc.level, got, tc.want)
		}
	}
}

func TestAnthropicModelGenerationParse(t *testing.T) {
	cases := []struct {
		model        string
		major, minor int
		ok           bool
	}{
		{"claude-opus-4-6", 4, 6, true},
		{"claude-sonnet-5", 5, 0, true},
		{"claude-haiku-4-5", 4, 5, true},
		{"claude-3-5-sonnet", 3, 5, true},
		{"custom", 0, 0, false},
	}
	for _, tc := range cases {
		maj, min, ok := anthropicModelGeneration(tc.model)
		if maj != tc.major || min != tc.minor || ok != tc.ok {
			t.Errorf("%s: got %d,%d,%v want %d,%d,%v", tc.model, maj, min, ok, tc.major, tc.minor, tc.ok)
		}
	}
}
