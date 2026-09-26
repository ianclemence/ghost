package scheduled

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsedSchedule represents a parsed natural language schedule.
type ParsedSchedule struct {
	Schedule    Schedule
	Title       string
	Timezone    string
	IsRecurring bool
	IsOneTime   bool
}

// ParseNaturalLanguage parses a natural language schedule description.
// It returns a ParsedSchedule or an error if the input cannot be parsed.
func ParseNaturalLanguage(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, error) {
	if timezone == "" {
		timezone = "UTC"
	}

	input = strings.TrimSpace(strings.ToLower(input))

	// Try to parse recurring schedules first
	if schedule, ok := parseRecurringSchedule(input, referenceTime, timezone); ok {
		return schedule, nil
	}

	// Try to parse one-time schedules
	if schedule, ok := parseOneTimeSchedule(input, referenceTime, timezone); ok {
		return schedule, nil
	}

	return nil, fmt.Errorf("cannot parse schedule from: %s", input)
}

// parseRecurringSchedule parses recurring schedule patterns.
func parseRecurringSchedule(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, bool) {
	// "every day at 8 AM"
	everyDayRe := regexp.MustCompile(`every\s+day\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyDayRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * *", minute, hour),
			},
			Title:       fmt.Sprintf("Every day at %s", formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every Monday at 9 AM"
	weekdayMap := map[string]string{
		"monday": "1", "tuesday": "2", "wednesday": "3",
		"thursday": "4", "friday": "5", "saturday": "6", "sunday": "0",
	}
	everyWeekdayRe := regexp.MustCompile(`every\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyWeekdayRe.FindStringSubmatch(input); len(matches) >= 4 {
		dayNum := weekdayMap[matches[1]]
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * %s", minute, hour, dayNum),
			},
			Title:       fmt.Sprintf("Every %s at %s", matches[1], formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every week at 9 AM"
	everyWeekRe := regexp.MustCompile(`every\s+week\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyWeekRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * 1", minute, hour), // Monday
			},
			Title:       fmt.Sprintf("Every week at %s", formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every month on the 1st at 9 AM"
	everyMonthRe := regexp.MustCompile(`every\s+month\s+(?:on\s+)?(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyMonthRe.FindStringSubmatch(input); len(matches) >= 4 {
		day, _ := strconv.Atoi(matches[1])
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d %d * *", minute, hour, day),
			},
			Title:       fmt.Sprintf("Every month on the %d at %s", day, formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every N hours"
	everyNHoursRe := regexp.MustCompile(`every\s+(\d+)\s+hours?`)
	if matches := everyNHoursRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		interval := time.Duration(n) * time.Hour

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind:  ScheduleEvery,
				Every: interval,
			},
			Title:       fmt.Sprintf("Every %d hours", n),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every N minutes"
	everyNMinutesRe := regexp.MustCompile(`every\s+(\d+)\s+minutes?`)
	if matches := everyNMinutesRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		interval := time.Duration(n) * time.Minute

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind:  ScheduleEvery,
				Every: interval,
			},
			Title:       fmt.Sprintf("Every %d minutes", n),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	return nil, false
}

// defaultReminderHour is the hour a day-only phrase resolves to. An owner who
// names a day but no clock still means a time; the start of that day is the
// documented convention, and the confirmation states it back so it is visible.
const defaultReminderHour = 9

// weekdayMap is the parser's weekday vocabulary, shared by every day-shaped
// pattern so they can never disagree.
var weekdayMap = map[string]time.Weekday{
	"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday,
}

// monthMap is the month vocabulary. Abbreviations are matched by prefix, so
// "sept", "september" and "sep" all resolve.
var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

func monthFromToken(tok string) (time.Month, bool) {
	tok = strings.TrimRight(strings.ToLower(tok), ".")
	if len(tok) < 3 {
		return 0, false
	}
	m, ok := monthMap[tok[:3]]
	return m, ok
}

// Absolute-date grammar. An explicit calendar date is the most specific time
// signal an owner can give, and before this existed it was inexpressible: the
// only day shapes were relative ("friday", "tomorrow"), so "9 October" fell
// through and every attempt snapped to the nearest matching weekday — live,
// "remind me a day before the Chelsea game" landed a week early twice.
var (
	// "2026-10-09"
	isoDateRE = regexp.MustCompile(`\b(\d{4})-(\d{1,2})-(\d{1,2})\b`)
	// "9 october", "9th of october 2026", "on the 9th oct"
	dateDayFirstRE = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s*,?\s*(\d{4})?`)
	// "october 9", "oct 9, 2026"
	dateMonthFirstRE = regexp.MustCompile(`\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?\s*,?\s*(\d{4})?`)
	// an explicit clock: "9am", "at 9", "at 9:30 pm"
	clockAMPMRE = regexp.MustCompile(`(?:at\s+)?\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`)
	clockAtRE   = regexp.MustCompile(`\bat\s+(\d{1,2})(?::(\d{2}))?\b`)
	// a time-of-day word, for when the owner names the shape of the day
	timeOfDayRE = regexp.MustCompile(`\b(morning|afternoon|evening|night|tonight|noon|midnight)\b`)
)

// parseClockPhrase finds the clock the owner attached, if any: an explicit
// am/pm time, an "at <hour>" time, or a time-of-day word. It never guesses a
// bare number, so a day number cannot be mistaken for an hour.
func parseClockPhrase(input string) (int, int, bool) {
	if m := clockAMPMRE.FindStringSubmatch(input); len(m) >= 4 {
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		return parseHour(m[1], m[3]), minute, true
	}
	if m := clockAtRE.FindStringSubmatch(input); len(m) >= 2 {
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		return parseHour(m[1], ""), minute, true
	}
	if m := timeOfDayRE.FindStringSubmatch(input); len(m) >= 2 {
		switch m[1] {
		case "morning":
			return 9, 0, true
		case "afternoon":
			return 14, 0, true
		case "evening":
			return 19, 0, true
		case "tonight":
			// "Tonight" is this evening, not late night; the default matches
			// the promise resolver so both read one phrase the same way.
			return 20, 0, true
		case "night":
			return 21, 0, true
		case "noon":
			return 12, 0, true
		case "midnight":
			return 0, 0, true
		}
	}
	return 0, 0, false
}

// resolveAbsoluteDate resolves an explicit calendar date with an optional
// clock. A date with no clock uses the documented default hour.
//
// A missing year means the nearest future occurrence: a month and day is an
// annual date, and a reminder must never silently land in the past. A year the
// owner actually stated is honoured exactly, even if it has already gone.
func resolveAbsoluteDate(input string, ref time.Time, loc *time.Location) (time.Time, string, bool) {
	var (
		year, day          int
		month              time.Month
		matched, yearGiven bool
	)
	switch {
	case isoDateRE.MatchString(input):
		m := isoDateRE.FindStringSubmatch(input)
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo < 1 || mo > 12 || d < 1 || d > 31 {
			return time.Time{}, "", false
		}
		year, month, day, matched, yearGiven = y, time.Month(mo), d, true, true
	case dateDayFirstRE.MatchString(input):
		m := dateDayFirstRE.FindStringSubmatch(input)
		mo, ok := monthFromToken(m[2])
		if !ok {
			return time.Time{}, "", false
		}
		d, _ := strconv.Atoi(m[1])
		if d < 1 || d > 31 {
			return time.Time{}, "", false
		}
		month, day, matched = mo, d, true
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
			yearGiven = true
		}
	case dateMonthFirstRE.MatchString(input):
		m := dateMonthFirstRE.FindStringSubmatch(input)
		mo, ok := monthFromToken(m[1])
		if !ok {
			return time.Time{}, "", false
		}
		d, _ := strconv.Atoi(m[2])
		if d < 1 || d > 31 {
			return time.Time{}, "", false
		}
		month, day, matched = mo, d, true
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
			yearGiven = true
		}
	}
	if !matched {
		return time.Time{}, "", false
	}
	if !yearGiven {
		year = ref.Year()
	}
	hour, minute, ok := parseClockPhrase(input)
	if !ok {
		hour, minute = defaultReminderHour, 0
	}
	at := time.Date(year, month, day, hour, minute, 0, 0, loc)
	// time.Date normalises an out-of-range day (31 February becomes March);
	// a date that does not round-trip is one the owner did not mean, so it is
	// refused rather than silently stored as a different day.
	if at.Day() != day || at.Month() != month || at.Year() != year {
		return time.Time{}, "", false
	}
	if !yearGiven && !at.After(ref) {
		at = at.AddDate(1, 0, 0)
	}
	return at, at.Format("2 January 2006"), true
}

// resolveBareDayWord resolves a day word with no clock ("tomorrow", "friday",
// "tonight", "next week"). It runs last so every more specific shape above
// still wins. Each returns the documented default hour unless the owner named
// a clock or a time of day.
func resolveBareDayWord(input string, ref time.Time, loc *time.Location) (time.Time, string, bool) {
	hour, minute, hasClock := parseClockPhrase(input)
	pick := func(t time.Time, title string) (time.Time, string, bool) {
		h, m := defaultReminderHour, 0
		if hasClock {
			h, m = hour, minute
		}
		return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, loc), title, true
	}
	switch {
	case regexp.MustCompile(`\btomorrow\b`).MatchString(input):
		return pick(ref.AddDate(0, 0, 1), "Tomorrow")
	case regexp.MustCompile(`\btonight\b`).MatchString(input):
		return pick(ref, "Tonight")
	case regexp.MustCompile(`\btoday\b`).MatchString(input):
		return pick(ref, "Today")
	case regexp.MustCompile(`\bnext\s+week\b`).MatchString(input):
		return pick(ref.AddDate(0, 0, 7), "Next week")
	}
	for word, wd := range weekdayMap {
		if !regexp.MustCompile(`\b` + word + `\b`).MatchString(input) {
			continue
		}
		daysUntil := (int(wd) - int(ref.Weekday()) + 7) % 7
		if daysUntil == 0 {
			daysUntil = 7
		}
		return pick(ref.AddDate(0, 0, daysUntil), "Next "+word)
	}
	return time.Time{}, "", false
}

// parseOneTimeSchedule parses one-time schedule patterns.
func parseOneTimeSchedule(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, bool) {
	// Wall-clock times ("9 PM", "3 PM") mean 9 PM in the target timezone,
	// not UTC. Resolve the location once and compute dates in it, so a
	// reminder set in Asia/Bangkok fires at 9 PM there.
	loc, err := time.LoadLocation(timezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	ref := referenceTime.In(loc)

	// An explicit calendar date is the most specific signal there is.
	if at, title, ok := resolveAbsoluteDate(input, ref, loc); ok {
		return &ParsedSchedule{
			Schedule:  Schedule{Kind: ScheduleAt, At: &at},
			Title:     title,
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "tomorrow at 9 AM"
	tomorrowRe := regexp.MustCompile(`tomorrow\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := tomorrowRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		nextDay := ref.AddDate(0, 0, 1)
		at := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Tomorrow at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "today at 3 PM"
	todayRe := regexp.MustCompile(`today\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := todayRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		at := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Today at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "Friday at 3 PM"
	weekdayRe := regexp.MustCompile(`(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := weekdayRe.FindStringSubmatch(input); len(matches) >= 4 {
		targetDay := weekdayMap[matches[1]]
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		// Find next occurrence of this weekday
		daysUntil := (int(targetDay) - int(ref.Weekday()) + 7) % 7
		if daysUntil == 0 {
			daysUntil = 7 // Next week if same day
		}
		nextDay := ref.AddDate(0, 0, daysUntil)
		at := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Next %s at %s", matches[1], formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "in 2 hours"
	inHoursRe := regexp.MustCompile(`in\s+(\d+)\s+hours?`)
	if matches := inHoursRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		at := referenceTime.Add(time.Duration(n) * time.Hour)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("In %d hours", n),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "in 30 minutes"
	inMinutesRe := regexp.MustCompile(`in\s+(\d+)\s+minutes?`)
	if matches := inMinutesRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		at := referenceTime.Add(time.Duration(n) * time.Minute)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("In %d minutes", n),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// Time-before-day: "at 8:30 PM today", "at 9pm tonight", "tonight at
	// 9", "at 9 on Friday". Owners (and the model) naturally put the clock
	// before the day word; before these shapes existed, every such
	// phrasing failed with "I couldn't understand the schedule" — which
	// broke live reminders ("set dinner at 9pm tonight" → error → a
	// mangled retry).
	timeTodayRe := regexp.MustCompile(`(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s+today\b`)
	if matches := timeTodayRe.FindStringSubmatch(input); len(matches) >= 4 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		at := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Today at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "at 9pm tonight" / "tonight at 9" — tonight implies the evening, so
	// a bare hour without am/pm lands on the PM side (9 tonight = 9 PM).
	timeTonightRe := regexp.MustCompile(`(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s+tonight\b`)
	tonightAtRe := regexp.MustCompile(`tonight\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	for _, matches := range [][]string{
		timeTonightRe.FindStringSubmatch(input),
		tonightAtRe.FindStringSubmatch(input),
	} {
		if len(matches) < 4 {
			continue
		}
		hour := parseHour(matches[1], matches[3])
		if matches[3] == "" && hour < 12 {
			hour += 12
		}
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		at := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Tonight at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "at 9 on Friday" — time before the weekday (optional "on").
	timeWeekdayRe := regexp.MustCompile(`(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s+(?:on\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
	if matches := timeWeekdayRe.FindStringSubmatch(input); len(matches) >= 5 {
		targetDay := weekdayMap[matches[4]]
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		// Find next occurrence of this weekday (same rule as above).
		daysUntil := (int(targetDay) - int(ref.Weekday()) + 7) % 7
		if daysUntil == 0 {
			daysUntil = 7 // Next week if same day
		}
		nextDay := ref.AddDate(0, 0, daysUntil)
		at := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, loc)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Next %s at %s", matches[4], formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// A day word with no clock ("tomorrow", "friday", "tonight", "next week").
	// Last, so every more precise shape above has already had its chance.
	if at, title, ok := resolveBareDayWord(input, ref, loc); ok {
		return &ParsedSchedule{
			Schedule:  Schedule{Kind: ScheduleAt, At: &at},
			Title:     title,
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	return nil, false
}

// computeNextDaily computes the next daily occurrence at the given time.
func computeNextDaily(referenceTime time.Time, hour, minute int, timezone string) time.Time {
	loc, err := time.LoadLocation(timezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	ref := referenceTime.In(loc)
	nextDay := ref.AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, loc)
}

// parseHour parses an hour string with AM/PM indicator.
func parseHour(hourStr, ampm string) int {
	hour, _ := strconv.Atoi(hourStr)
	if strings.ToLower(ampm) == "pm" && hour < 12 {
		hour += 12
	} else if strings.ToLower(ampm) == "am" && hour == 12 {
		hour = 0
	}
	return hour
}

// formatTime formats hour and minute into a human-readable string.
func formatTimeDisplay(hour, minute int) string {
	ampm := "AM"
	h := hour
	if hour >= 12 {
		ampm = "PM"
		if hour > 12 {
			h = hour - 12
		}
	} else if hour == 0 {
		h = 12
	}

	if minute == 0 {
		return fmt.Sprintf("%d %s", h, ampm)
	}
	return fmt.Sprintf("%d:%02d %s", h, minute, ampm)
}
