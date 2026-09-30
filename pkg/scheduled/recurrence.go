package scheduled

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Recurrence recognition. People say the same schedule in many shapes:
//
//	every weekday at 8am          weekdays at 8
//	8am every day                 every morning
//	every Monday and Thursday at 7pm    Mondays at 9
//	every other day               every 3 days           hourly
//
// The old parser knew four fixed sentences ("every day at N", "every <weekday>
// at N", "every week at N", "every month on the Nth at N") and nothing else, so
// "every weekday at 8am to take my vitamins" was not understood at all and
// Ghost asked "what should happen?" about a request that had already said what.
// This finds the cadence and the time of day independently, anywhere in the
// sentence, and reports which words it used so the caller can remove them and
// keep the rest as the task.

// Recurrence is a recurring schedule found in a sentence.
type Recurrence struct {
	Parsed *ParsedSchedule
	// Spans are the exact words that expressed the schedule (cadence and time
	// of day), for removal to leave the task.
	Spans []string
	// Clause is the schedule in plain words, lower case: "every weekday at 8:00 AM".
	Clause string
}

const dayTok = `(?:mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nes(?:day)?)?|thu(?:r(?:s(?:day)?)?)?|fri(?:day)?|sat(?:ur(?:day)?)?|sun(?:day)?)s?`

var (
	intervalRE  = regexp.MustCompile(`(?i)\bevery\s+(\d+)\s+(minutes?|mins?|hours?|hrs?|days?|weeks?)\b`)
	everyUnitRE = regexp.MustCompile(`(?i)\b(?:every\s+(?:other\s+)?(hour|day)|hourly)\b`)
	monthlyRE   = regexp.MustCompile(`(?i)\b(?:every\s+month|monthly|each\s+month)(?:\s+on)?(?:\s+the)?(?:\s+(\d{1,2})(?:st|nd|rd|th)?)?\b`)
	rangeRE     = regexp.MustCompile(`(?i)\b(?:every\s+|on\s+)?(` + dayTok + `)\s*(?:to|through|thru|until|-)\s*(` + dayTok + `)\b`)
	weekdaysRE  = regexp.MustCompile(`(?i)\b(?:(?:every|each|on)\s+(?:the\s+)?(?:week\s*days?|work\s*days?|business\s+days?)|week\s*days|work\s*days|business\s+days)\b`)
	weekendsRE  = regexp.MustCompile(`(?i)\b(?:(?:every|each|on)\s+(?:the\s+)?weekends?|weekends)\b`)
	daylistRE   = regexp.MustCompile(`(?i)\b(?:(?:every|each|on)\s+(?:the\s+)?)?(` + dayTok + `(?:\s*(?:,|and|&|\+|/)\s*` + dayTok + `)*)\b`)
	dailyRE     = regexp.MustCompile(`(?i)\b(?:every\s*day|each\s+day|daily|every\s+(?:morning|afternoon|evening|night)|nightly)\b`)
	weeklyRE    = regexp.MustCompile(`(?i)\b(?:every\s+week|each\s+week|weekly)\b`)
)

var dayNumber = map[string]int{"mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6, "sun": 0}

func dayNum(tok string) (int, bool) {
	tok = strings.ToLower(strings.TrimSpace(tok))
	if len(tok) < 3 {
		return 0, false
	}
	n, ok := dayNumber[tok[:3]]
	return n, ok
}

// ParseRecurrence finds a recurring schedule in text. ok is false when the
// sentence does not describe one.
func ParseRecurrence(text string, ref time.Time, timezone string) (*Recurrence, bool) {
	if timezone == "" {
		timezone = "UTC"
	}
	in := strings.TrimSpace(text)
	if in == "" {
		return nil, false
	}
	mk := func(sched Schedule, cadenceSpan, phrase string, clockUsed bool, h, m int, timeSpan string) (*Recurrence, bool) {
		spans := []string{cadenceSpan}
		if clockUsed && timeSpan != "" {
			spans = append(spans, timeSpan)
		}
		clause := phrase
		if sched.Kind == ScheduleCron {
			// The time is always stated back, including the default a bare
			// "every day" resolves to, so it is never a surprise.
			clause += " at " + formatTimeDisplay(h, m)
		}
		return &Recurrence{
			Parsed: &ParsedSchedule{
				Schedule:    sched,
				Title:       strings.ToUpper(clause[:1]) + clause[1:],
				Timezone:    timezone,
				IsRecurring: true,
			},
			Spans:  spans,
			Clause: clause,
		}, true
	}

	// Fixed intervals: no time of day.
	if m := intervalRE.FindStringSubmatch(in); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 {
			return nil, false
		}
		unit := strings.ToLower(m[2])
		var d time.Duration
		var word string
		switch {
		case strings.HasPrefix(unit, "min"):
			d, word = time.Duration(n)*time.Minute, "minute"
		case strings.HasPrefix(unit, "h"):
			d, word = time.Duration(n)*time.Hour, "hour"
		case strings.HasPrefix(unit, "d"):
			d, word = time.Duration(n)*24*time.Hour, "day"
		default:
			d, word = time.Duration(n)*7*24*time.Hour, "week"
		}
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return mk(Schedule{Kind: ScheduleEvery, Every: d}, intervalRE.FindString(in),
			fmt.Sprintf("every %d %s%s", n, word, plural), false, 0, 0, "")
	}
	if m := everyUnitRE.FindStringSubmatch(in); m != nil {
		unit := strings.ToLower(m[1])
		if unit == "" { // "hourly"
			unit = "hour"
		}
		span := everyUnitRE.FindString(in)
		other := strings.Contains(strings.ToLower(span), "other")
		switch {
		case unit == "hour":
			return mk(Schedule{Kind: ScheduleEvery, Every: time.Hour}, span, "every hour", false, 0, 0, "")
		case other:
			return mk(Schedule{Kind: ScheduleEvery, Every: 48 * time.Hour}, span, "every other day", false, 0, 0, "")
		}
		// "every day" continues below as a daily time-of-day schedule.
	}

	h, mi, haveClock := parseClockPhrase(strings.ToLower(in))
	if !haveClock {
		h, mi = defaultReminderHour, 0
	}
	timeSpan := ""
	if haveClock {
		low := strings.ToLower(in)
		switch {
		case clockAMPMRE.MatchString(low):
			timeSpan = clockAMPMRE.FindString(low)
		case clockAtRE.MatchString(low):
			timeSpan = clockAtRE.FindString(low)
		default:
			timeSpan = timeOfDayRE.FindString(low)
		}
	}
	cron := func(dom, dow string) Schedule {
		return Schedule{Kind: ScheduleCron, Expr: fmt.Sprintf("%d %d %s * %s", mi, h, dom, dow)}
	}

	if m := monthlyRE.FindStringSubmatch(in); m != nil {
		day := 1
		if m[1] != "" {
			day, _ = strconv.Atoi(m[1])
		}
		if day < 1 || day > 31 {
			return nil, false
		}
		return mk(cron(strconv.Itoa(day), "*"), monthlyRE.FindString(in),
			fmt.Sprintf("every month on the %d", day), haveClock, h, mi, timeSpan)
	}
	if span := weekdaysRE.FindString(in); span != "" {
		return mk(cron("*", "1-5"), span, "every weekday", haveClock, h, mi, timeSpan)
	}
	if span := weekendsRE.FindString(in); span != "" {
		return mk(cron("*", "0,6"), span, "every weekend", haveClock, h, mi, timeSpan)
	}
	if m := rangeRE.FindStringSubmatch(in); m != nil {
		a, aok := dayNum(m[1])
		b, bok := dayNum(m[2])
		if aok && bok {
			var days []string
			for d := a; ; d = (d + 1) % 7 {
				days = append(days, strconv.Itoa(d))
				if d == b || len(days) > 7 {
					break
				}
			}
			return mk(cron("*", strings.Join(days, ",")), rangeRE.FindString(in),
				fmt.Sprintf("every %s to %s", dayName(a), dayName(b)), haveClock, h, mi, timeSpan)
		}
	}
	if dailyRE.MatchString(in) {
		return mk(cron("*", "*"), dailyRE.FindString(in), "every day", haveClock, h, mi, timeSpan)
	}
	// Named days need a word that makes them recurring ("every", "each",
	// "on") or a plural ("Mondays"); a bare "Monday" is a one-off.
	if m := daylistRE.FindStringSubmatch(in); m != nil {
		full := daylistRE.FindString(in)
		lowFull := strings.ToLower(full)
		// "on Friday" is one Friday; only "every"/"each" or a plural ("Fridays",
		// "on Mondays and Thursdays") repeats.
		list := strings.ToLower(strings.TrimSpace(m[1]))
		recurring := strings.HasPrefix(lowFull, "every") || strings.HasPrefix(lowFull, "each") ||
			strings.HasSuffix(list, "s") && !strings.HasSuffix(list, "ss")
		if recurring {
			seen := map[int]bool{}
			var nums []int
			for _, tok := range regexp.MustCompile(`(?i)`+dayTok).FindAllString(m[1], -1) {
				if n, ok := dayNum(tok); ok && !seen[n] {
					seen[n] = true
					nums = append(nums, n)
				}
			}
			if len(nums) > 0 {
				sort.Ints(nums)
				var expr, names []string
				for _, n := range nums {
					expr = append(expr, strconv.Itoa(n))
					names = append(names, dayName(n))
				}
				return mk(cron("*", strings.Join(expr, ",")), full, "every "+joinNames(names), haveClock, h, mi, timeSpan)
			}
		}
	}
	if weeklyRE.MatchString(in) {
		return mk(cron("*", "1"), weeklyRE.FindString(in), "every week", haveClock, h, mi, timeSpan)
	}
	return nil, false
}

func dayName(n int) string {
	return [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}[n%7]
}

func joinNames(n []string) string {
	switch len(n) {
	case 0:
		return ""
	case 1:
		return n[0]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

// RemoveSpans deletes the schedule words from text, case-insensitively, and
// tidies what is left. What remains is the task.
func RemoveSpans(text string, spans []string) string {
	out := text
	for _, s := range spans {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(s)).ReplaceAllString(out, " ")
	}
	return strings.Join(strings.Fields(out), " ")
}
