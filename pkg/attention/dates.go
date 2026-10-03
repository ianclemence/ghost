package attention

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Span is a stretch of days a memory talks about: a trip from 26 Oct to
// 6 Nov, or a single day (Start == End).
type Span struct {
	Start, End time.Time // local midnight in the owner's zone
}

var months = map[string]time.Month{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3,
	"apr": 4, "april": 4, "may": 5, "jun": 6, "june": 6, "jul": 7, "july": 7,
	"aug": 8, "august": 8, "sep": 9, "sept": 9, "september": 9, "oct": 10, "october": 10,
	"nov": 11, "november": 11, "dec": 12, "december": 12,
}

// "26 October", "26 Oct 2026", "Saturday 10 October", "October 26", "Oct 26, 2026", "2026-10-26".
var (
	dayMonthRE = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sept?(?:ember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\b\.?(?:,?\s+(\d{4}))?`)
	monthDayRE = regexp.MustCompile(`(?i)\b(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sept?(?:ember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4}))?`)
	isoRE      = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
)

type found struct {
	at  time.Time
	pos int
	yr  bool
}

// FindSpan reads the dates a sentence names and returns the span they cover.
// A date without a year is the next one on or after `heard` (when the owner
// said it), so "26 October" said on 2 Oct is this year's and "3 March" is
// next year's. Text that names no date returns false: Ghost never guesses a
// date from "next month" or "soon".
//
// Only the first two dates count (start and end). Words like "scrapped" or
// "cancelled" next to a date are not read: callers pass the current belief.
func FindSpan(text string, heard time.Time, loc *time.Location) (Span, bool) {
	if loc == nil {
		loc = time.Local
	}
	var all []found
	add := func(y int, m time.Month, d int, pos int, hasYear bool) {
		if d < 1 || d > 31 || m < 1 || m > 12 {
			return
		}
		t := time.Date(y, m, d, 0, 0, 0, 0, loc)
		if t.Day() != d { // 31 Feb and friends
			return
		}
		all = append(all, found{at: t, pos: pos, yr: hasYear})
	}
	base := heard.In(loc)
	for _, m := range dayMonthRE.FindAllStringSubmatchIndex(text, -1) {
		d, _ := strconv.Atoi(text[m[2]:m[3]])
		mon := months[strings.ToLower(text[m[4]:m[5]])]
		y, has := base.Year(), false
		if m[6] >= 0 {
			y, _ = strconv.Atoi(text[m[6]:m[7]])
			has = true
		}
		add(y, mon, d, m[0], has)
	}
	for _, m := range monthDayRE.FindAllStringSubmatchIndex(text, -1) {
		mon := months[strings.ToLower(text[m[2]:m[3]])]
		d, _ := strconv.Atoi(text[m[4]:m[5]])
		y, has := base.Year(), false
		if m[6] >= 0 {
			y, _ = strconv.Atoi(text[m[6]:m[7]])
			has = true
		}
		add(y, mon, d, m[0], has)
	}
	for _, m := range isoRE.FindAllStringSubmatchIndex(text, -1) {
		y, _ := strconv.Atoi(text[m[2]:m[3]])
		mo, _ := strconv.Atoi(text[m[4]:m[5]])
		d, _ := strconv.Atoi(text[m[6]:m[7]])
		add(y, time.Month(mo), d, m[0], true)
	}
	if len(all) == 0 {
		return Span{}, false
	}
	// Reading order; two patterns can match the same words ("Oct 26 2026"
	// is also "26 2026"…), so keep the first of any that start together.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].pos < all[i].pos {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	var dates []found
	for _, f := range all {
		if len(dates) > 0 && f.pos-dates[len(dates)-1].pos < 3 {
			continue
		}
		dates = append(dates, f)
		if len(dates) == 2 {
			break
		}
	}
	heardDay := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc)
	for i := range dates {
		if !dates[i].yr && dates[i].at.Before(heardDay.AddDate(0, 0, -1)) {
			dates[i].at = dates[i].at.AddDate(1, 0, 0)
		}
	}
	sp := Span{Start: dates[0].at, End: dates[0].at}
	if len(dates) == 2 {
		end := dates[1].at
		if !dates[1].yr && end.Before(sp.Start) {
			end = end.AddDate(1, 0, 0)
		}
		// A second date far from the first is another fact, not an end.
		if !end.Before(sp.Start) && end.Sub(sp.Start) <= 120*24*time.Hour {
			sp.End = end
		}
	}
	return sp, true
}
