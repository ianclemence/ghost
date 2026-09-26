package scheduled

import (
	"testing"
	"time"
)

// The anchor from the live failure: Saturday 26 September 2026, 10:00 in the
// owner's timezone (Asia/Bangkok).
func absoluteRef(t *testing.T) (time.Time, string) {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	return time.Date(2026, 9, 26, 10, 0, 0, 0, loc), "Asia/Bangkok"
}

// An explicit date must resolve to that date. Before this existed, "9 October"
// was inexpressible and every attempt snapped to the nearest weekday.
func TestParseAbsoluteDates(t *testing.T) {
	ref, tz := absoluteRef(t)
	loc, _ := time.LoadLocation(tz)

	cases := []struct {
		input string
		want  time.Time
	}{
		// The owner's actual failure: the Chelsea match is Sat 10 Oct 2026,
		// so "a day before" must be expressible as 9 October.
		{"remind me about the chelsea game on 9 october at 9 am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		{"9 october at 9 am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		{"9 october 2026 at 9 am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		{"on the 9th of october at 9 am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		{"9th october at 09:30", time.Date(2026, 10, 9, 9, 30, 0, 0, loc)},
		{"october 9 at 9 am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		{"oct 9 at 9pm", time.Date(2026, 10, 9, 21, 0, 0, 0, loc)},
		{"9 oct at 21:00", time.Date(2026, 10, 9, 21, 0, 0, 0, loc)},
		{"2026-10-09 at 9am", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		// A month + day with no clock still resolves; a reminder needs a time,
		// and the documented default is the start of that day.
		{"9 october", time.Date(2026, 10, 9, 9, 0, 0, 0, loc)},
		// An explicit year is honoured exactly, even in the past: the owner
		// was specific.
		{"9 october 2020 at 9 am", time.Date(2020, 10, 9, 9, 0, 0, 0, loc)},
		// No year and already passed: the nearest future occurrence, so a
		// reminder never silently lands in the past.
		{"9 september", time.Date(2027, 9, 9, 9, 0, 0, 0, loc)},
	}
	for _, tc := range cases {
		got, err := ParseNaturalLanguage(tc.input, ref, tz)
		if err != nil || got == nil {
			t.Errorf("ParseNaturalLanguage(%q) failed: %v", tc.input, err)
			continue
		}
		if got.Schedule.At == nil || !got.Schedule.At.Equal(tc.want) {
			t.Errorf("ParseNaturalLanguage(%q) = %v, want %v", tc.input, got.Schedule.At, tc.want)
		}
		if !got.IsOneTime {
			t.Errorf("ParseNaturalLanguage(%q) must be one-time", tc.input)
		}
	}
}

// Bare day words resolve without the model having to invent a clock.
func TestParseBareDayWords(t *testing.T) {
	ref, tz := absoluteRef(t)
	loc, _ := time.LoadLocation(tz)

	cases := []struct {
		input string
		want  time.Time
	}{
		{"remind me tomorrow", time.Date(2026, 9, 27, 9, 0, 0, 0, loc)},
		{"tomorrow", time.Date(2026, 9, 27, 9, 0, 0, 0, loc)},
		{"tomorrow morning", time.Date(2026, 9, 27, 9, 0, 0, 0, loc)},
		{"tomorrow evening", time.Date(2026, 9, 27, 19, 0, 0, 0, loc)},
		{"remind me friday", time.Date(2026, 10, 2, 9, 0, 0, 0, loc)},
		{"friday afternoon", time.Date(2026, 10, 2, 14, 0, 0, 0, loc)},
		{"tonight", time.Date(2026, 9, 26, 20, 0, 0, 0, loc)},
		{"remind me tonight", time.Date(2026, 9, 26, 20, 0, 0, 0, loc)},
		{"next week", time.Date(2026, 10, 3, 9, 0, 0, 0, loc)},
	}
	for _, tc := range cases {
		got, err := ParseNaturalLanguage(tc.input, ref, tz)
		if err != nil || got == nil || got.Schedule.At == nil {
			t.Errorf("ParseNaturalLanguage(%q) failed: %v", tc.input, err)
			continue
		}
		if !got.Schedule.At.Equal(tc.want) {
			t.Errorf("ParseNaturalLanguage(%q) = %v, want %v", tc.input, got.Schedule.At, tc.want)
		}
	}
}

// The precise shapes that already worked must keep working unchanged.
func TestParseExistingShapesStillWork(t *testing.T) {
	ref, tz := absoluteRef(t)
	loc, _ := time.LoadLocation(tz)

	cases := []struct {
		input string
		want  time.Time
	}{
		{"tomorrow at 9 AM", time.Date(2026, 9, 27, 9, 0, 0, 0, loc)},
		{"today at 3 PM", time.Date(2026, 9, 26, 15, 0, 0, 0, loc)},
		{"friday at 3 PM", time.Date(2026, 10, 2, 15, 0, 0, 0, loc)},
		{"at 8:30 PM today", time.Date(2026, 9, 26, 20, 30, 0, 0, loc)},
		{"tonight at 9", time.Date(2026, 9, 26, 21, 0, 0, 0, loc)},
		{"at 9 on Friday", time.Date(2026, 10, 2, 9, 0, 0, 0, loc)},
	}
	for _, tc := range cases {
		got, err := ParseNaturalLanguage(tc.input, ref, tz)
		if err != nil || got == nil || got.Schedule.At == nil {
			t.Errorf("ParseNaturalLanguage(%q) failed: %v", tc.input, err)
			continue
		}
		if !got.Schedule.At.Equal(tc.want) {
			t.Errorf("ParseNaturalLanguage(%q) = %v, want %v", tc.input, got.Schedule.At, tc.want)
		}
	}
}

// Recurring shapes are unaffected by the new one-time grammar.
func TestRecurringUnaffectedByAbsoluteDates(t *testing.T) {
	ref, tz := absoluteRef(t)
	got, err := ParseNaturalLanguage("every day at 8 AM", ref, tz)
	if err != nil || got == nil || !got.IsRecurring {
		t.Fatalf("recurring parse broken: %+v (%v)", got, err)
	}
	got2, err := ParseNaturalLanguage("every monday at 9 am", ref, tz)
	if err != nil || got2 == nil || !got2.IsRecurring {
		t.Fatalf("recurring weekday parse broken: %+v (%v)", got2, err)
	}
}

// A phrase with no resolvable time must still fail, so callers can ask rather
// than guess. "a day before" is the live case.
func TestUnresolvableTimeStillFails(t *testing.T) {
	ref, tz := absoluteRef(t)
	for _, input := range []string{
		"remind me about the chelsea game a day before",
		"remind me to call sam",
		"remind me before the game",
	} {
		if got, err := ParseNaturalLanguage(input, ref, tz); err == nil && got != nil {
			t.Errorf("ParseNaturalLanguage(%q) resolved %v; it must not invent a time", input, got.Schedule.At)
		}
	}
}
