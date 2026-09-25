package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

// The summary outlives the day it was written: it must pin the clock and
// forbid relative dates, or "tomorrow (Thu, Sep 24)" freezes into durable
// context and is read back days later as a lie.
func TestSummarizePromptPinsClockAndAbsoluteDates(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 30, 0, 0, time.Local)
	at := mustParse(t, "2026-09-23T15:45:00+07:00")
	batch := []providers.Message{
		{Role: "user", Content: "remind me tomorrow", CreatedAt: at},
		{Role: "assistant", Content: "done", CreatedAt: at},
		{Role: "user", Content: "undated line"},
	}
	p := summarizePrompt(batch, "owner asked for reminders tomorrow", now)

	checks := []string{
		"Current date and time: Friday, 2026-09-25 14:30",
		"absolute dates only",
		`never "today", "tomorrow"`,
		"[2026-09-23 15:45] remind me tomorrow",
		"rewrite any relative dates",
		"\nuser: undated line\n",
	}
	for _, want := range checks {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, p)
		}
	}
	// A message with no known time is passed through without a stamp.
	if strings.Contains(p, "] undated line") {
		t.Error("undated message must not be given a fabricated date")
	}
}
