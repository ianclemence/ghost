package tools

import (
	"context"
	"strings"
	"testing"
)

// The item title must name the ACTION, not the clock. Time phrases that
// lead the content — and their to/that/about connectors — are cut, but
// only when day-anchored: a bare "9 am meeting" is content, not a
// schedule. Regression: "Remind me today at 9 PM that dinner with Jas…"
// stored the literal title "Today at 9 PM that dinner with Jas is at
// 9 PM."
func TestExtractReminderContentStripsSchedulePhrase(t *testing.T) {
	cases := map[string]string{
		// The " to " path stays untouched.
		"Remind me tomorrow at 9 AM to call the dentist": "call the dentist",
		// Time-first with a parenthetical date and an "about" connector.
		"Remind me at 9pm tonight (Friday Sep 25) about dinner with Jas.": "dinner with Jas.",
		// Time-first with a "that" connector.
		"Remind me at 8:30 PM today that dinner with Jas is at 9 PM.": "dinner with Jas is at 9 PM.",
		// Day-first with a "that" connector.
		"Remind me today at 9 PM that dinner with Jas is at 9 PM.": "dinner with Jas is at 9 PM.",
	}
	for in, want := range cases {
		if got := extractReminderContent(in); got != want {
			t.Errorf("extractReminderContent(%q) = %q, want %q", in, got, want)
		}
	}
}

// A time phrase without a day anchor is content, not a schedule — it
// must survive stripping so the title keeps the information the owner
// put there.
func TestExtractReminderContentKeepsUnanchoredTime(t *testing.T) {
	got := extractReminderContent("Bring the 9 am meeting notes")
	if got != "Bring the 9 am meeting notes" {
		t.Errorf("unanchored time was cut: %q", got)
	}
}

// Full chain: the phrasing that actually produced the mangled store
// must now title cleanly.
func TestDerivedTitleFromMangledPhrasing(t *testing.T) {
	got := derivedItemTitle(extractReminderContent("Remind me today at 9 PM that dinner with Jas is at 9 PM."))
	if want := "Dinner with Jas is at 9 PM."; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// The parse failure must name accepted shapes WITHOUT echoing the
// content: content carries its own time phrase, and the old splice
// produced garbage the model then read back ("tomorrow at 9 AM to at
// 9pm tonight …").
func TestScheduleParseErrorNamesShapesWithoutContent(t *testing.T) {
	tool := NewScheduleTool(newFakeScheduleService(), "Asia/Bangkok")
	tool.SetContext("mobile", "default")

	res := tool.Execute(context.Background(), map[string]interface{}{
		"message": "Remind me sometime soon about the thing",
	})
	if !res.IsError {
		t.Fatalf("want a parse error, got success: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "the thing") {
		t.Errorf("error must not echo the content: %s", res.ForLLM)
	}
	for _, want := range []string{"tomorrow at 9 AM", "at 8:30 PM today", "tonight at 9"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("error must name shape %q: %s", want, res.ForLLM)
		}
	}
}
