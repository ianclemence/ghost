package heartbeat

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cadence gating for the heartbeat model turn.
//
// Measured on the live device: the heartbeat was running the ENTIRE
// HEARTBEAT.md checklist through a full model turn every 30 minutes, and the
// turn almost always ended in "HEARTBEAT_OK" — 22,589 to 45,524 prompt tokens
// per beat, 48 beats a day, for a file whose sections are explicitly cadenced
// ("Morning Routine (08:00)", "Maintenance (Every 4 Hours)", "Continuous
// Learning (Weekly)").
//
// Nothing here is a new scheduler: the scheduler still owns all timed work.
// This is a cheap deterministic filter that puts only the sections whose
// cadence is actually due in front of the model, so a tick with nothing due
// costs no model call at all. Every checklist still runs on its own cadence.

var (
	clockWindowRE = regexp.MustCompile(`\((\d{1,2}):(\d{2})\b`)
	everyHoursRE  = regexp.MustCompile(`(?i)\(\s*every\s+(\d+)\s*hours?`)
	weeklyRE      = regexp.MustCompile(`(?i)\(\s*(weekly|every week)`)
	dailyRE       = regexp.MustCompile(`(?i)\(\s*(daily|every day)`)
	hourlyRE      = regexp.MustCompile(`(?i)\(\s*(hourly|every hour)`)
)

// lastRunPath is where the most recent model-backed heartbeat is recorded.
// Persisted so a restart does not re-run a section that already ran today.
func lastRunPath(workspace string) string {
	return filepath.Join(workspace, "state", "heartbeat-last-run")
}

// LoadLastRun reads the last model-backed heartbeat time, zero when unknown.
func LoadLastRun(workspace string) time.Time {
	raw, err := os.ReadFile(lastRunPath(workspace))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// MarkRan records that the model-backed heartbeat ran at now.
func MarkRan(workspace string, now time.Time) {
	path := lastRunPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(now.UTC().Format(time.RFC3339)), 0600)
}

// splitSections splits HEARTBEAT.md into a preamble plus one entry per level-2
// heading. Content before the first heading is the preamble and is always kept
// (it carries the guardrails the whole file depends on).
func splitSections(content string) (string, []string) {
	lines := strings.Split(content, "\n")
	var preamble []string
	var sections []string
	current := -1
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			sections = append(sections, line)
			current = len(sections) - 1
			continue
		}
		if current < 0 {
			preamble = append(preamble, line)
			continue
		}
		sections[current] += "\n" + line
	}
	return strings.TrimRight(strings.Join(preamble, "\n"), "\n"), sections
}

// sectionDue reports whether a section whose heading carries a cadence is due.
// An unrecognised cadence is treated as due, so new prose is never silently
// skipped.
func sectionDue(heading string, now time.Time, loc *time.Location, lastRun time.Time) bool {
	local := now.In(loc)
	switch {
	case clockWindowRE.MatchString(heading):
		m := clockWindowRE.FindStringSubmatch(heading)
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		target := time.Date(local.Year(), local.Month(), local.Day(), h, min, 0, 0, loc)
		if lastRun.IsZero() {
			// First run of a process: run a daily window only if it is
			// already past today, so a fresh boot does not skip the morning.
			return !local.Before(target)
		}
		return !target.After(local) && target.After(lastRun.In(loc))
	case everyHoursRE.MatchString(heading):
		m := everyHoursRE.FindStringSubmatch(heading)
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			return true
		}
		return lastRun.IsZero() || now.Sub(lastRun) >= time.Duration(n)*time.Hour
	case hourlyRE.MatchString(heading):
		return lastRun.IsZero() || now.Sub(lastRun) >= time.Hour
	case weeklyRE.MatchString(heading):
		return lastRun.IsZero() || now.Sub(lastRun) >= 7*24*time.Hour
	case dailyRE.MatchString(heading):
		if lastRun.IsZero() {
			return true
		}
		return lastRun.In(loc).Format("2006-01-02") != local.Format("2006-01-02")
	default:
		return true
	}
}

// DueContent returns the heartbeat prompt for this tick: the always-on
// preamble plus only the sections whose cadence has come around. An empty
// result means nothing is due and the tick needs no model call.
func DueContent(content string, now time.Time, loc *time.Location, lastRun time.Time) string {
	preamble, sections := splitSections(content)
	if len(sections) == 0 {
		// No headings: the file is one undifferentiated block. Treat it as
		// always due rather than silently disabling it.
		return content
	}
	var keep []string
	for _, s := range sections {
		heading := s
		if i := strings.IndexByte(s, '\n'); i > 0 {
			heading = s[:i]
		}
		if sectionDue(heading, now, loc, lastRun) {
			keep = append(keep, s)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	out := preamble
	if out != "" {
		out += "\n\n"
	}
	return out + strings.Join(keep, "\n\n")
}
