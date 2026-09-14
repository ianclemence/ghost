package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutboxEnqueueDueRoundTrip(t *testing.T) {
	ws := t.TempDir()
	nt := Notice{Topic: "test:1", Priority: 9, Urgency: true, Confidence: 0.9, DedupeKey: "t1", Message: "hello"}
	enqueueHeld(ws, nt, time.Now().UTC().Add(-time.Minute))
	due := dueHeld(ws, time.Now().UTC())
	if len(due) != 1 || due[0].Message != "hello" {
		t.Fatalf("expected 1 due notice, got %+v", due)
	}
	if rest := dueHeld(ws, time.Now().UTC()); len(rest) != 0 {
		t.Fatalf("outbox must drain, got %+v", rest)
	}
}

func TestOutboxHoldsFuture(t *testing.T) {
	ws := t.TempDir()
	nt := Notice{Topic: "test:2", Priority: 5, Confidence: 0.9, DedupeKey: "t2", Message: "later"}
	enqueueHeld(ws, nt, time.Now().UTC().Add(time.Hour))
	if due := dueHeld(ws, time.Now().UTC()); len(due) != 0 {
		t.Fatalf("future notice delivered early: %+v", due)
	}
}

func TestOutboxBounded(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < outboxMaxItems+10; i++ {
		enqueueHeld(ws, Notice{Topic: "t", Message: "m"}, time.Now().UTC())
	}
	raw, err := os.ReadFile(filepath.Join(ws, "proactive", outboxFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, l := range splitLines(string(raw)) {
		if l != "" {
			lines++
		}
	}
	if lines > outboxMaxItems {
		t.Fatalf("outbox unbounded: %d lines", lines)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
