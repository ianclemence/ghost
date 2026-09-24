package tools

import (
	"strings"
	"testing"
)

func TestBackgroundLogLifecycle(t *testing.T) {
	l := NewBackgroundLog()
	id := l.Start("sess", "spawn", "research")
	if id == "" {
		t.Fatal("Start must return an id")
	}
	running := l.Running("sess")
	if len(running) != 1 || running[0].Label != "research" || running[0].Tool != "spawn" {
		t.Fatalf("running wrong: %+v", running)
	}
	if len(l.Running("other")) != 0 {
		t.Fatal("sessions must not leak into each other")
	}
	l.Finish("sess", id, true, "found it")
	if len(l.Running("sess")) != 0 {
		t.Fatal("finish must clear running")
	}
	done := l.DrainDone("sess")
	if len(done) != 1 || !done[0].OK || done[0].Result != "found it" || done[0].Label != "research" {
		t.Fatalf("done wrong: %+v", done)
	}
	if len(l.DrainDone("sess")) != 0 {
		t.Fatal("drain must be exactly-once")
	}
}

func TestBackgroundLogUnstart(t *testing.T) {
	l := NewBackgroundLog()
	id := l.Start("sess", "spawn", "")
	l.Unstart("sess", id)
	if len(l.Running("sess")) != 0 || len(l.DrainDone("sess")) != 0 {
		t.Fatal("inline-finishing tools must leave no trace")
	}
}

func TestBackgroundLogLabelFallback(t *testing.T) {
	l := NewBackgroundLog()
	l.Start("sess", "spawn", "")
	if got := l.Running("sess"); len(got) != 1 || got[0].Label != "spawn" {
		t.Fatalf("label must fall back to tool name: %+v", got)
	}
}

func TestBackgroundLogTruncates(t *testing.T) {
	l := NewBackgroundLog()
	id := l.Start("sess", "spawn", "big")
	l.Finish("sess", id, true, strings.Repeat("x", maxBackgroundResultChars+500))
	done := l.DrainDone("sess")
	if len(done) != 1 || len(done[0].Result) > maxBackgroundResultChars+10 {
		t.Fatalf("result must be bounded, got %d chars", len(done[0].Result))
	}
}

func TestBackgroundLogNilSafe(t *testing.T) {
	var l *BackgroundLog
	if l.Start("s", "t", "x") != "" {
		t.Fatal("nil log Start must return empty")
	}
	l.Unstart("s", "x")
	l.Finish("s", "x", true, "y")
	if len(l.Running("s")) != 0 || len(l.DrainDone("s")) != 0 {
		t.Fatal("nil log must stay empty")
	}
}
