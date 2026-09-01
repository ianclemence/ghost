package personalcontext

import (
	"testing"
	"time"
)

// TestCommunicationStyleBehaviorProfile verifies the behavioral-profile
// extraction: response style (single word) plus format cues Ghost should follow.
func TestCommunicationStyleBehaviorProfile(t *testing.T) {
	style := []struct {
		text string
		ok   bool
	}{
		{"I prefer concise answers", true},
		{"I prefer detailed answers", true},
		{"I live in Bangkok", false},
	}
	for _, c := range style {
		if (communicationStyleRegex.MatchString(c.text)) != c.ok {
			t.Errorf("style(%q): matched=%v, want %v", c.text, communicationStyleRegex.MatchString(c.text), c.ok)
		}
	}

	format := []struct {
		text string
		ok   bool
	}{
		{"I want bullet points", true},
		{"I like options", true},
		{"I like to be asked before deleting", true},
		{"I prefer a summary", true},
		{"I want a table format", true},
		{"I live in Bangkok", false},
		{"my goal is to run a marathon", false},
	}
	for _, c := range format {
		if (communicationFormatRegex.MatchString(c.text)) != c.ok {
			t.Errorf("format(%q): matched=%v, want %v", c.text, communicationFormatRegex.MatchString(c.text), c.ok)
		}
	}
}

// TestExtractCommunicationFormatStoresStyle ensures the format cues persist as a
// communication.style entry that the digest surfaces.
func TestExtractCommunicationFormatStoresStyle(t *testing.T) {
	store, _ := Open(t.TempDir())
	now := time.Now().UTC()
	if _, err := Apply(store, Input{SessionID: "s", MessageID: "m1", Text: "I want bullet points", Timestamp: now}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	found := false
	for _, e := range store.Current() {
		if e.Predicate == "preference/communication.style" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a communication.style entry for a format cue")
	}
}
