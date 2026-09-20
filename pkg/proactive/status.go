// Proactive status: the owner-facing view of Ghost's quiet work.
//
// Ghost reaches out on its own only when something is genuinely useful, and
// the policy (PROACTIVE_PREFERENCES.md) bounds how often. That machinery is
// invisible by default, which makes Ghost look idle when it is actually
// watching. This projection makes it legible: are we in quiet hours, how much
// of today's budget is left, and is anything waiting to be delivered.
//
// It is read-only. It changes no policy and delivers nothing.
package proactive

import "time"

// Status is the owner-safe snapshot of proactivity. All fields are derived
// from the policy and the noticer/outbox state; nothing here is a claim.
type Status struct {
	// Quiet reports whether now falls in the quiet window.
	Quiet bool `json:"quiet"`
	// QuietStart / QuietEnd are the "HH:MM" bounds in the user's timezone.
	QuietStart string `json:"quiet_start,omitempty"`
	QuietEnd   string `json:"quiet_end,omitempty"`
	// BudgetUsed / BudgetMax are today's non-urgent check-ins. Max may be 0
	// when the policy does not set one (unbounded).
	BudgetUsed int `json:"budget_used"`
	BudgetMax  int `json:"budget_max"`
	// Waiting counts notices held for later delivery (quiet hours, no live
	// session, offline). They will be delivered when the window opens.
	Waiting int `json:"waiting"`
	// NextBriefing / NextReflection are "HH:MM" when configured.
	NextBriefing   string `json:"next_briefing,omitempty"`
	NextReflection string `json:"next_reflection,omitempty"`
}

// Active reports whether Ghost is doing anything proactive the owner should
// know about: a live budget, a quiet window, or waiting notices.
func (s Status) Active() bool {
	return s.Waiting > 0 || s.BudgetMax > 0 || s.Quiet
}

// BuildStatus assembles the snapshot from a parsed policy, the current time,
// the user's location, today's push count, and the waiting-notice count.
func BuildStatus(p Policy, now time.Time, loc *time.Location, used, waiting int) Status {
	s := Status{
		Quiet:          InQuietHours(now, loc, p),
		BudgetUsed:     used,
		BudgetMax:      p.MaxPushesPerDay,
		Waiting:        waiting,
		NextBriefing:   p.MorningBriefing,
		NextReflection: p.EveningReflection,
	}
	if p.QuietStart != p.QuietEnd {
		s.QuietStart = clock(p.QuietStart)
		s.QuietEnd = clock(p.QuietEnd)
	}
	return s
}

func clock(minutes int) string {
	h := minutes / 60
	m := minutes % 60
	return pad2(h) + ":" + pad2(m)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
