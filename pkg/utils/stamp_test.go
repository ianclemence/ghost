package utils

import "testing"

// The internal history label is stripped only as a LEADING prefix,
// repeatedly (an eager model can echo it twice), and never mid-text —
// where a bracketed date is the author's content.
func TestStripDateStamp(t *testing.T) {
	cases := map[string]string{
		"[2026-09-25 15:59] Set — dinner Friday":      "Set — dinner Friday",
		"[2026-09-25 15:59]Set — dinner":              "Set — dinner",
		"[2026-09-25 15:59] [2026-09-25 16:00] Twice": "Twice",
		"No label here":                     "No label here",
		"[Done] ready":                      "[Done] ready",
		"mid text [2026-09-25 15:59] stays": "mid text [2026-09-25 15:59] stays",
		"":                                  "",
		"[2026-99-99 99:99] shape beats calendar check": "shape beats calendar check",
	}
	for in, want := range cases {
		if got := StripDateStamp(in); got != want {
			t.Errorf("StripDateStamp(%q) = %q, want %q", in, got, want)
		}
	}
}

// CouldStartHistoryStamp is the streaming gate: only text that can still
// grow into a label is held. Anything else must flush immediately — a
// reply not beginning with "[" never waits a chunk.
func TestCouldStartHistoryStamp(t *testing.T) {
	hold := []string{
		"",
		"[",
		"[2026",
		"[2026-09-25 15",
		"[2026-09-25 15:59",
		"[2026-09-25 15:59] ",
	}
	for _, s := range hold {
		if !CouldStartHistoryStamp(s) {
			t.Errorf("CouldStartHistoryStamp(%q) = false, want hold", s)
		}
	}
	pass := []string{
		"Hello",
		"[Done] ready",
		"2026 was fine",
		"[2026-09-25 15:59] done", // past the template length
		"x[2026-09-25 15:59]",
	}
	for _, s := range pass {
		if CouldStartHistoryStamp(s) {
			t.Errorf("CouldStartHistoryStamp(%q) = true, want pass", s)
		}
	}
}
