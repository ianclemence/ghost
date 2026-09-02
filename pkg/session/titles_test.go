package session

import (
	"testing"
)

func TestGenerateTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "New conversation"},
		{"   ", "New conversation"},
		{"Hey Ghost, what's the weather like?", "What's the weather like"},
		{"hi ghost, can you help me with my code?", "Can you help me with my code"},
		{"Hello Ghost, tell me a story about a dragon.", "Tell me a story about a dragon"},
		{"What's 2+2?", "What's 2+2"},
		{"Can you explain how TCP/IP works?", "Can you explain how TCP/IP works"},
		{"hey ghost, what time is it in Bangkok right now?", "What time is it in Bangkok right now"},
		{"This is a very long message that should be truncated because it exceeds the maximum length limit we set for titles.", "This is a very long message that should be truncated"},
		{"Short", "Short"},
		{"hey ghost, extra spaces after greeting", "Extra spaces after greeting"},
	}
	for _, tc := range cases {
		got := GenerateTitle(tc.input)
		if got != tc.want {
			t.Errorf("GenerateTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGenerateSummary(t *testing.T) {
	cases := []struct {
		messages []messageForSummary
		want     string
	}{
		{nil, ""},
		{[]messageForSummary{}, ""},
		{
			[]messageForSummary{
				{Role: "user", Content: "What's the weather in Bangkok?"},
			},
			"What's the weather in Bangkok?",
		},
		{
			[]messageForSummary{
				{Role: "user", Content: "What's the weather in Bangkok?"},
				{Role: "assistant", Content: "The weather in Bangkok is sunny and 32 degrees."},
			},
			"What's the weather in Bangkok? — The weather in Bangkok is sunny and 32 degrees",
		},
		{
			[]messageForSummary{
				{Role: "assistant", Content: "Hello! How can I help?"},
				{Role: "user", Content: "Tell me a joke"},
				{Role: "assistant", Content: "Why did the chicken cross the road? To get to the other side."},
			},
			"Tell me a joke — Why did the chicken cross the road? To get to the other side",
		},
	}
	for _, tc := range cases {
		got := GenerateSummary(tc.messages)
		if got != tc.want {
			t.Errorf("GenerateSummary(...) = %q, want %q", got, tc.want)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  string
	}{
		{"Short", 20, "Short"},
		{"This is a longer title that needs truncation", 20, "This is a longer ti\u2026"},
		{"Hello World", 5, "Hell\u2026"},
		{"Hello World", 11, "Hello World"},
	}
	for _, tc := range cases {
		got := TruncateTitle(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("TruncateTitle(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func TestFormatSessionTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "New conversation"},
		{"Weather in Bangkok", "Weather in Bangkok"},
	}
	for _, tc := range cases {
		got := FormatSessionTitle(tc.input)
		if got != tc.want {
			t.Errorf("FormatSessionTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestIsEmptyOrWhitespace(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"hello", false},
		{" hello ", false},
	}
	for _, tc := range cases {
		got := IsEmptyOrWhitespace(tc.input)
		if got != tc.want {
			t.Errorf("IsEmptyOrWhitespace(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsTitlePunctuated(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"Hello", false},
		{"Hello.", true},
		{"Hello!", true},
		{"Hello?", true},
	}
	for _, tc := range cases {
		got := IsTitlePunctuated(tc.input)
		if got != tc.want {
			t.Errorf("IsTitlePunctuated(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestShouldUpdateTitle(t *testing.T) {
	cases := []struct {
		existingTitle string
		messageCount  int
		want          bool
	}{
		{"", 0, true},                    // No title
		{"New conversation", 0, true},     // Default title
		{"Weather in Bangkok", 3, false},  // Too early
		{"Weather in Bangkok", 5, true},   // First evolution
		{"Weather in Bangkok", 10, false}, // Not at 10 yet
		{"Weather in Bangkok", 15, false}, // Not at 10 boundary
		{"Weather in Bangkok", 20, true},  // At 20 (10*2)
		{"Weather in Bangkok", 30, true},  // At 30 (10*3)
	}
	for _, tc := range cases {
		got := ShouldUpdateTitle(tc.existingTitle, tc.messageCount)
		if got != tc.want {
			t.Errorf("ShouldUpdateTitle(%q, %d) = %v, want %v", tc.existingTitle, tc.messageCount, got, tc.want)
		}
	}
}

func TestIsMeaningfulTitleChange(t *testing.T) {
	cases := []struct {
		oldTitle string
		newTitle string
		want     bool
	}{
		{"Weather", "Weather", false},           // Same
		{"Weather.", "Weather", false},           // Punctuation only
		{"Weather in Bangkok", "Weather in Bangkok.", false}, // Punctuation only
		{"Weather in Bangkok", "Weather in Bangkok today", false}, // Minor addition
		{"Weather in Bangkok", "Japan Trip Planning", true},     // Different topic
		{"Weather", "Japan Trip Planning", true},                // Different topic
	}
	for _, tc := range cases {
		got := IsMeaningfulTitleChange(tc.oldTitle, tc.newTitle)
		if got != tc.want {
			t.Errorf("IsMeaningfulTitleChange(%q, %q) = %v, want %v", tc.oldTitle, tc.newTitle, got, tc.want)
		}
	}
}

func TestEvolveTitle(t *testing.T) {
	cases := []struct {
		currentTitle string
		messages     []messageForSummary
		want         string
	}{
		{
			"Weather in Bangkok",
			[]messageForSummary{
				{Role: "user", Content: "What's the weather?"},
				{Role: "assistant", Content: "It's sunny."},
			},
			"Weather in Bangkok", // Not enough messages
		},
		{
			"Weather in Bangkok",
			[]messageForSummary{
				{Role: "user", Content: "What's the weather?"},
				{Role: "assistant", Content: "It's sunny."},
				{Role: "user", Content: "Now help me plan a trip to Japan"},
				{Role: "assistant", Content: "Sure! When are you going?"},
				{Role: "user", Content: "I want to visit Tokyo and Osaka"},
			},
			"Now help me plan a trip to Japan", // Topic shifted
		},
	}
	for _, tc := range cases {
		got := EvolveTitle(tc.currentTitle, tc.messages)
		if got != tc.want {
			t.Errorf("EvolveTitle(%q, ...) = %q, want %q", tc.currentTitle, got, tc.want)
		}
	}
}

func TestIsTopicRelated(t *testing.T) {
	cases := []struct {
		title string
		topic string
		want  bool
	}{
		{"Weather in Bangkok", "What's the weather in Bangkok?", true},  // Same topic
		{"Weather in Bangkok", "How's the weather today?", true},         // Related
		{"Weather in Bangkok", "Help me plan a trip to Japan", false},    // Different
		{"Japan Trip Planning", "What to pack for October?", false},      // Different (packing vs planning)
		{"Japan Trip Planning", "Trip to Tokyo and Osaka", true},         // Related (trip keyword)
	}
	for _, tc := range cases {
		got := IsTopicRelated(tc.title, tc.topic)
		if got != tc.want {
			t.Errorf("IsTopicRelated(%q, %q) = %v, want %v", tc.title, tc.topic, got, tc.want)
		}
	}
}
