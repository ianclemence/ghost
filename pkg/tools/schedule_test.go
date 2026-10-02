package tools

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

func TestReminderSentenceIgnoresADiscardedDate(t *testing.T) {
	owner := "yeah the 16th is the new plan, forget the october 26 dates. can you remind me to book the flights by this sunday evening?"
	got := reminderSentence(owner)
	if got != "can you remind me to book the flights by this sunday evening" {
		t.Fatalf("got %q", got)
	}
	ref := time.Date(2026, 10, 2, 12, 9, 0, 0, time.UTC)
	p, err := scheduled.ParseNaturalLanguage(got, ref, "Asia/Bangkok")
	if err != nil || p == nil || p.Schedule.At.Day() != 4 {
		t.Fatalf("want Sunday Oct 4, got %v %v", p, err)
	}
}
