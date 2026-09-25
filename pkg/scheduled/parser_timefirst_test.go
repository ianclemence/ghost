package scheduled

import (
	"testing"
	"time"
)

// Time-before-day phrasing is how people (and the model) actually talk:
// "at 8:30 PM today", "at 9pm tonight", "tonight at 9", "at 9pm on
// Monday". Before these shapes existed every one failed with "I couldn't
// understand the schedule" — which broke live reminders: the dinner
// reschedule to 8:30 errored, the model retried badly, and the turn
// collapsed into an approval detour.
func TestParseTimeBeforeDayShapes(t *testing.T) {
	// Friday, Sep 25 2026.
	ref := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	timezone := "UTC"

	fri2030 := time.Date(2026, 9, 25, 20, 30, 0, 0, time.UTC)
	fri2100 := time.Date(2026, 9, 25, 21, 0, 0, 0, time.UTC)
	mon2100 := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)

	cases := []struct {
		input string
		want  time.Time
	}{
		{"remind me at 8:30 PM today that dinner with jas is at 9 PM.", fri2030},
		{"remind me at 9pm tonight (friday sep 25) about dinner with jas.", fri2100},
		{"tonight at 9", fri2100},
		{"at 9 tonight", fri2100},
		{"at 9:15 tonight", time.Date(2026, 9, 25, 21, 15, 0, 0, time.UTC)},
		{"remind me at 9pm on monday to review the budget", mon2100},
	}
	for _, tt := range cases {
		res, err := ParseNaturalLanguage(tt.input, ref, timezone)
		if err != nil {
			t.Errorf("ParseNaturalLanguage(%q): %v", tt.input, err)
			continue
		}
		if res.Schedule.Kind != ScheduleAt || res.Schedule.At == nil {
			t.Errorf("ParseNaturalLanguage(%q) kind = %s, want at", tt.input, res.Schedule.Kind)
			continue
		}
		if !res.Schedule.At.Equal(tt.want) {
			t.Errorf("ParseNaturalLanguage(%q) at = %v, want %v", tt.input, res.Schedule.At, tt.want)
		}
		if !res.IsOneTime {
			t.Errorf("ParseNaturalLanguage(%q) must be one-time", tt.input)
		}
	}
}

// "tonight" with no meridiem means the evening — 9 tonight is 9 PM, not
// 9 AM.
func TestParseTonightDefaultsEvening(t *testing.T) {
	ref := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	res, err := ParseNaturalLanguage("tonight at 9", ref, "UTC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := res.Schedule.At.Hour(); got != 21 {
		t.Errorf("tonight at 9 hour = %d, want 21", got)
	}
}
