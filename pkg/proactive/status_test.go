package proactive

import (
	"testing"
	"time"
)

func TestBuildStatusQuietWindow(t *testing.T) {
	loc := time.UTC
	p := defaults() // quiet 23:00-08:00
	night := time.Date(2026, 3, 1, 2, 0, 0, 0, loc)
	day := time.Date(2026, 3, 1, 12, 0, 0, 0, loc)

	nightStatus := BuildStatus(p, night, loc, 0, 0)
	if !nightStatus.Quiet {
		t.Errorf("02:00 must be quiet")
	}
	if nightStatus.QuietStart != "23:00" || nightStatus.QuietEnd != "08:00" {
		t.Errorf("quiet bounds wrong: %q-%q", nightStatus.QuietStart, nightStatus.QuietEnd)
	}
	dayStatus := BuildStatus(p, day, loc, 0, 0)
	if dayStatus.Quiet {
		t.Errorf("12:00 must not be quiet")
	}
}

func TestBuildStatusBudgetAndWaiting(t *testing.T) {
	loc := time.UTC
	p := defaults()
	p.MaxPushesPerDay = 5
	s := BuildStatus(p, time.Date(2026, 3, 1, 12, 0, 0, 0, loc), loc, 2, 3)
	if s.BudgetUsed != 2 || s.BudgetMax != 5 {
		t.Errorf("budget wrong: %d/%d", s.BudgetUsed, s.BudgetMax)
	}
	if s.Waiting != 3 {
		t.Errorf("waiting wrong: %d", s.Waiting)
	}
	if s.NextBriefing != "08:00" || s.NextReflection != "22:00" {
		t.Errorf("cadence wrong: %q / %q", s.NextBriefing, s.NextReflection)
	}
}

func TestStatusActive(t *testing.T) {
	if (Status{}).Active() {
		t.Errorf("an empty status is not active")
	}
	if !(Status{Waiting: 1}).Active() {
		t.Errorf("waiting notices make it active")
	}
	if !(Status{Quiet: true}).Active() {
		t.Errorf("a quiet window makes it active")
	}
}

func TestBuildStatusNoQuietWhenBoundsEqual(t *testing.T) {
	loc := time.UTC
	p := defaults()
	p.QuietStart = 0
	p.QuietEnd = 0
	s := BuildStatus(p, time.Date(2026, 3, 1, 2, 0, 0, 0, loc), loc, 0, 0)
	if s.Quiet {
		t.Errorf("equal bounds mean no quiet hours")
	}
	if s.QuietStart != "" || s.QuietEnd != "" {
		t.Errorf("no quiet bounds should be reported: %q-%q", s.QuietStart, s.QuietEnd)
	}
}
