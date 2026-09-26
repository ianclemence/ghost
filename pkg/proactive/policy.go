// Package proactive reads PROACTIVE_PREFERENCES.md as deterministic policy:
// quiet hours, budgets, cooldowns, cadence. The heartbeat and noticer
// consume it; the model never interprets it. Quiet wins unless urgent.
package proactive

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Policy is the parsed proactivity contract.
type Policy struct {
	// QuietStart/QuietEnd are minutes since midnight in the user timezone.
	QuietStart int
	QuietEnd   int
	// MaxPushesPerDay caps non-urgent pushes (urgent exempt).
	MaxPushesPerDay int
	// CooldownPerTopic silences a topic after a push.
	CooldownPerTopic time.Duration
	// DedupeWindow suppresses identical content.
	DedupeWindow time.Duration
	// MorningBriefing / EveningReflection are HH:MM in user timezone.
	MorningBriefing   string
	EveningReflection string

	// Enabled is the master switch for proactive suggestions and notices.
	// A disabled policy still lets explicitly requested work (reminders the
	// owner asked for) run — it only silences Ghost volunteering things.
	Enabled bool
	// Categories limits which opportunity categories may surface. Empty means
	// all categories; names match ideas.ObsKind.Category() (reminders,
	// routines, goals, tasks).
	Categories []string
	// PreferredChannel, when set, overrides the last-active channel for
	// proactive delivery. Empty means "wherever the owner was last".
	PreferredChannel string
	// ProposalTTLHours bounds how long a surfaced proposal stays answerable.
	ProposalTTLHours int
}

func defaults() Policy {
	return Policy{
		QuietStart: 23 * 60, QuietEnd: 8 * 60,
		MaxPushesPerDay: 5,
		CooldownPerTopic: 6 * time.Hour,
		DedupeWindow:     24 * time.Hour,
		MorningBriefing:   "08:00",
		EveningReflection: "22:00",

		Enabled:          true,
		Categories:       nil,
		PreferredChannel: "",
		ProposalTTLHours: 24,
	}
}

var (
	quietRE   = regexp.MustCompile("(?m)`quiet_hours:\\s*([0-9]{1,2}):([0-9]{2})\\s*-\\s*([0-9]{1,2}):([0-9]{2})`")
	maxPushRE = regexp.MustCompile("(?m)`max_pushes_per_day:\\s*(\\d+)`")
	coolRE    = regexp.MustCompile("(?m)`cooldown_per_topic:\\s*(\\d+)h`")
	dedupeRE  = regexp.MustCompile("(?m)`dedupe_window:\\s*(\\d+)h`")
	mornRE    = regexp.MustCompile("(?m)`morning_briefing:\\s*([0-9]{1,2}:[0-9]{2})`")
	eveRE     = regexp.MustCompile("(?m)`evening_reflection:\\s*([0-9]{1,2}:[0-9]{2})`")

	enabledRE  = regexp.MustCompile("(?m)`enabled:\\s*(true|false)`")
	catsRE     = regexp.MustCompile("(?m)`categories:\\s*([a-z_,\\s]+)`")
	channelRE  = regexp.MustCompile("(?m)`preferred_channel:\\s*([a-z_]+)`")
	propTTLRE  = regexp.MustCompile("(?m)`proposal_ttl_hours:\\s*(\\d+)`")
)

func toMin(h, m string) (int, bool) {
	hi, err1 := strconv.Atoi(h)
	mi, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hi < 0 || hi > 23 || mi < 0 || mi > 59 {
		return 0, false
	}
	return hi*60 + mi, true
}

// Load parses workspace/PROACTIVE_PREFERENCES.md. Missing file or values
// fall back to defaults; malformed values are ignored (never fail closed
// on a preferences file).
func Load(workspace string) Policy {
	p := defaults()
	data, err := os.ReadFile(filepath.Join(workspace, "PROACTIVE_PREFERENCES.md"))
	if err != nil {
		return p
	}
	s := string(data)
	if m := quietRE.FindStringSubmatch(s); m != nil {
		if a, ok := toMin(m[1], m[2]); ok {
			if b, ok := toMin(m[3], m[4]); ok {
				p.QuietStart, p.QuietEnd = a, b
			}
		}
	}
	if m := maxPushRE.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n <= 50 {
			p.MaxPushesPerDay = n
		}
	}
	if m := coolRE.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 0 && n <= 72 {
			p.CooldownPerTopic = time.Duration(n) * time.Hour
		}
	}
	if m := dedupeRE.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 0 && n <= 168 {
			p.DedupeWindow = time.Duration(n) * time.Hour
		}
	}
	if m := mornRE.FindStringSubmatch(s); m != nil && validHM(m[1]) {
		p.MorningBriefing = m[1]
	}
	if m := eveRE.FindStringSubmatch(s); m != nil && validHM(m[1]) {
		p.EveningReflection = m[1]
	}
	if m := enabledRE.FindStringSubmatch(s); m != nil {
		p.Enabled = m[1] == "true"
	}
	if m := catsRE.FindStringSubmatch(s); m != nil {
		var cats []string
		for _, c := range strings.Split(m[1], ",") {
			c = strings.ToLower(strings.TrimSpace(c))
			if c != "" {
				cats = append(cats, c)
			}
		}
		p.Categories = cats
	}
	if m := channelRE.FindStringSubmatch(s); m != nil {
		p.PreferredChannel = strings.ToLower(strings.TrimSpace(m[1]))
	}
	if m := propTTLRE.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 168 {
			p.ProposalTTLHours = n
		}
	}
	return p
}

// CategoryAllowed reports whether a category may surface under this policy.
// An empty category list means every category is allowed.
func (p Policy) CategoryAllowed(category string) bool {
	if len(p.Categories) == 0 {
		// A single "none" entry is how an owner turns off every category
		// while leaving the master switch on.
		return true
	}
	for _, c := range p.Categories {
		if c == category {
			return true
		}
	}
	return false
}

func validHM(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return false
	}
	_, ok := toMin(parts[0], parts[1])
	return ok
}

// InQuietHours reports whether now (in loc) falls in the quiet window.
// Handles overnight wraps (23:00-08:00). Equal bounds mean no quiet hours.
func InQuietHours(now time.Time, loc *time.Location, p Policy) bool {
	if p.QuietStart == p.QuietEnd {
		return false
	}
	if loc == nil {
		loc = time.UTC
	}
	mins := now.In(loc).Hour()*60 + now.In(loc).Minute()
	if p.QuietStart < p.QuietEnd {
		return mins >= p.QuietStart && mins < p.QuietEnd
	}
	return mins >= p.QuietStart || mins < p.QuietEnd
}
