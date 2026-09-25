package agent

import "testing"

// The label must die at the stream: a reply starting with it emits only
// its content, whether the label arrives in one chunk or split across
// several (live streams split arbitrarily).
func TestStampStreamStripsLeadingLabel(t *testing.T) {
	s := &stampStream{}

	// One chunk carries the whole label plus content.
	out, ok := s.feed("[2026-09-25 15:59] Set — dinner")
	if !ok || out != "Set — dinner" {
		t.Fatalf("single chunk: got (%q, %v), want (\"Set — dinner\", true)", out, ok)
	}

	// A second stream splits the label: held, then flushed without it.
	s2 := &stampStream{}
	if out, ok := s2.feed("[2026-09-25 "); ok {
		t.Fatalf("partial label must be held, got (%q, %v)", out, ok)
	}
	out, ok = s2.feed("15:59] Set")
	if !ok || out != "Set" {
		t.Fatalf("split label: got (%q, %v), want (\"Set\", true)", out, ok)
	}

	// Exactly the label, nothing yet: emit nothing, stay ready.
	s3 := &stampStream{}
	if out, ok := s3.feed("[2026-09-25 15:59] "); !ok || out != "" {
		t.Fatalf("bare label: got (%q, %v), want (\"\", true)", out, ok)
	}
	if out, ok := s3.feed("Set"); !ok || out != "Set" {
		t.Fatalf("after bare label: got (%q, %v), want (\"Set\", true)", out, ok)
	}

	// An eager model that echoed the label twice: both go, content stays.
	s4 := &stampStream{}
	out, ok = s4.feed("[2026-09-25 15:59] [2026-09-25 16:00] Set")
	if !ok || out != "Set" {
		t.Fatalf("doubled label: got (%q, %v), want (\"Set\", true)", out, ok)
	}
}

// A reply that only LOOKS like the start of a label flushes the moment
// the bytes diverge — brackets are common, and holding real text is the
// one thing the stream must not do.
func TestStampStreamFlushesOnDivergence(t *testing.T) {
	s := &stampStream{}
	out, ok := s.feed("[Done] ready")
	if !ok || out != "[Done] ready" {
		t.Fatalf("bracket divergence: got (%q, %v), want immediate pass-through", out, ok)
	}

	s2 := &stampStream{}
	if _, ok := s2.feed("[2026"); ok {
		t.Fatalf("candidate must hold")
	}
	out, ok = s2.feed(" plans")
	if !ok || out != "[2026 plans" {
		t.Fatalf("late divergence: got (%q, %v), want (\"[2026 plans\", true)", out, ok)
	}
}

// The hold applies only to the START of one reply: once the stream is
// settled, a later bracketed date is ordinary content and passes.
func TestStampStreamSettledPassesEverything(t *testing.T) {
	s := &stampStream{}
	if _, ok := s.feed("Done"); !ok {
		t.Fatalf("first chunk must pass")
	}
	out, ok := s.feed("[2026-09-25 15:59] quoted")
	if !ok || out != "[2026-09-25 15:59] quoted" {
		t.Fatalf("settled stream: got (%q, %v), want pass-through", out, ok)
	}
}
