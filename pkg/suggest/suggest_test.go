package suggest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRulesAnswerTheOfferGhostMade(t *testing.T) {
	for last, want := range map[string]string{
		`"Check the prices for the Shenzhen trip flights" is the next one up. Want me to move it, or add a nudge before it?`: "Yes, move it",
		"Want me to keep watching BA123 and tell you if its gate, time or status changes?":                                   "Yes, go ahead",
		"I can check it again. Shall I do that now?":                                                                         "Yes, do that now",
		"Rain is around. Want a reminder to take an umbrella?":                                                               "",
		"Should I tell you when it lands?":                                                                                   "Yes, go ahead",
		"Should I tell you at 7?":                                                                                            "Yes, tell me at 7",
		"Do you want me to look at your calendar?":                                                                           "Yes, look at my calendar",
	} {
		got, ok := FromRules(last)
		if want == "" {
			if ok {
				t.Errorf("%q: expected no rule, got %q", last, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: got %q (%v) want %q", last, got, ok, want)
		}
	}
}

func TestRulesDoNotSayYesToAQuestionForInformation(t *testing.T) {
	for _, last := range []string{
		"Which city should I check?",
		"What time should I remind you?",
		"Sure. When should I schedule it?",
		"How should I word it?",
	} {
		if got, ok := FromRules(last); ok {
			t.Errorf("%q must not get %q: it asks for information, not permission", last, got)
		}
	}
}

func TestRulesForListsAndReports(t *testing.T) {
	if got, _ := FromRules("5 pending:\n- Pay rent — Sun"); got != "Move the first one" {
		t.Errorf("reminders list: %q", got)
	}
	if got, _ := FromRules("Weather in Bangkok: ☁️ 32.0°C, Overcast (via wttr.in)."); got != "What about tomorrow?" {
		t.Errorf("weather: %q", got)
	}
	if _, ok := FromRules("Here you go."); ok {
		t.Error("a plain statement has no rule")
	}
	if _, ok := FromRules("   "); ok {
		t.Error("an empty message has no rule")
	}
}

func TestEveryRuleFitsOneRow(t *testing.T) {
	long := "Want me to keep watching the flight every thirty minutes until it lands and tell you the moment its gate changes?"
	got, ok := FromRules(long)
	if !ok || utf8.RuneCountInString(got) > MaxLen {
		t.Errorf("a long offer must still produce a short answer, got %q", got)
	}
}

func TestCleanKeepsOneShortLineAndDropsTheRest(t *testing.T) {
	last := "Done. Your reminder is set for 6 PM."
	for in, want := range map[string]string{
		`  "Thanks, that's all"  `:             "Thanks, that's all",
		"Move it to 7pm\nbecause...":           "Move it to 7pm",
		"You: And the weather there?":          "And the weather there?",
		"**Remind me tomorrow**":               "Remind me tomorrow",
		"NONE":                                 "",
		"none.":                                "",
		"":                                     "",
		"x":                                    "",
		strings.Repeat("a", MaxLen+1):          "",
		"See https://example.com/a":            "",
		"{\"suggestion\": \"yes\"}":            "",
		"Done. Your reminder is set for 6 PM.": "",
	} {
		got, ok := Clean(in, last)
		if want == "" {
			if ok {
				t.Errorf("%q should be dropped, got %q", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: got %q (%v) want %q", in, got, ok, want)
		}
	}
}

func TestPromptClipsAndEndsOnTheOpenLine(t *testing.T) {
	sys, user := Prompt([]Turn{{"user", "hi"}, {"assistant", strings.Repeat("word ", 200)}})
	if !strings.Contains(sys, "NONE") || !strings.HasSuffix(user, "Next message from Person:") {
		t.Fatalf("prompt shape wrong:\n%s\n%s", sys, user)
	}
	if utf8.RuneCountInString(user) > 600 {
		t.Errorf("a long message must be clipped, prompt is %d characters", utf8.RuneCountInString(user))
	}
	if !strings.Contains(user, "Person: hi") || !strings.Contains(user, "Assistant: ") {
		t.Errorf("roles missing:\n%s", user)
	}
}

func TestCleanNeverSuggestsAGrant(t *testing.T) {
	for _, s := range []string{"Yes, approve", "Allow once", "deny", "Always allow"} {
		if got, ok := Clean(s, ""); ok {
			t.Errorf("Clean(%q) = %q; a grant must never be a suggestion", s, got)
		}
	}
	if _, ok := Clean("What about hotels?", ""); !ok {
		t.Errorf("an ordinary suggestion must pass")
	}
}
