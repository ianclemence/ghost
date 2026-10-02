package main

import (
	"testing"
	"time"
)

func TestComposerHintFollowsTheDayAndFitsARow(t *testing.T) {
	at := func(s string) time.Time {
		v, _ := time.Parse("2006-01-02T15:04", s)
		return v
	}
	if composerHint(at("2026-10-05T08:00")) == composerHint(at("2026-10-05T23:30")) {
		t.Error("morning and night must not say the same thing")
	}
	if composerHint(at("2026-10-05T09:00")) != composerHint(at("2026-10-05T10:30")) {
		t.Error("the hint must be stable within a part of the day")
	}
	seen := map[string]bool{}
	for d := 5; d < 8; d++ {
		seen[composerHint(at("2026-10-0"+string(rune('0'+d))+"T09:00"))] = true
	}
	if len(seen) < 2 {
		t.Error("the hint should change from one day to the next")
	}
	for h := 0; h < 24; h++ {
		for d := 1; d < 10; d++ {
			now := time.Date(2026, 10, d, h, 0, 0, 0, time.UTC)
			if n := len([]rune(composerHint(now))); n > 30 {
				t.Errorf("%q is %d characters; a hint must fit one row", composerHint(now), n)
			}
		}
	}
}
