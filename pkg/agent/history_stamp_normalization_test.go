package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// An older turn may have stored the model's own echo of the label
// (before the write boundary stripped it). Stamping must normalize that
// away and apply exactly ONE label — the row's real time — never stack
// a second one on top.
func TestStampHistoryNormalizesEchoedLabel(t *testing.T) {
	at := mustParse(t, "2026-09-25T16:00:00+07:00")
	history := []providers.Message{
		{Role: "assistant", Content: "[2026-09-25 15:59] Set — dinner Friday", CreatedAt: at},
	}

	got := stampHistory(history)

	want := "[2026-09-25 16:00] Set — dinner Friday"
	if got[0].Content != want {
		t.Errorf("normalized stamp = %q, want %q", got[0].Content, want)
	}
}

// Without a known time nothing is prepended — but an echoed label is
// still cleaned up, so the model never sees two label shapes on one row.
func TestStampHistoryStripsEchoWithoutKnownTime(t *testing.T) {
	history := []providers.Message{
		{Role: "assistant", Content: "[2026-09-25 15:59] Earlier"},
	}

	got := stampHistory(history)

	if got[0].Content != "Earlier" {
		t.Errorf("echo without CreatedAt = %q, want %q", got[0].Content, "Earlier")
	}
}
