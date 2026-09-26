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

	// Space-dropped label (model artifact: "[2026-09-2515:59] …"): the
	// shape is still a label imitation, so it strips too — in one chunk…
	s5 := &stampStream{}
	out, ok = s5.feed("[2026-09-2515:59] Approval")
	if !ok || out != "Approval" {
		t.Fatalf("spaceless label: got (%q, %v), want (\"Approval\", true)", out, ok)
	}

	// …and split across chunks, which is where the live leak happened.
	s6 := &stampStream{}
	if out, ok := s6.feed("[2026-09-251"); ok {
		t.Fatalf("partial spaceless label must be held, got (%q, %v)", out, ok)
	}
	out, ok = s6.feed("5:59] Approval")
	if !ok || out != "Approval" {
		t.Fatalf("split spaceless label: got (%q, %v), want (\"Approval\", true)", out, ok)
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

// The gate runs BEFORE the dump filter. Filtering first dropped the "["
// that opens the label (models emit it as its own chunk), the gate only
// ever saw the body, and "2026-09-2610:04] …" reached the live
// transcript as a bare date fragment (observed on v0.24.46).
func TestStampFilterStreamGateBeforeFilter(t *testing.T) {
	var got []string
	sink := stampFilterStream(func(s string) { got = append(got, s) })

	// Split label: "[" is held by the gate, not eaten by the filter;
	// the body completes it and only its content reaches the sink.
	sink("[")
	sink("2026-09-2610:04] Hello")
	if len(got) != 1 || got[0] != "Hello" {
		t.Fatalf("split label: got %q, want [Hello]", got)
	}

	// A whole label in one chunk strips the same way.
	got = nil
	sink = stampFilterStream(func(s string) { got = append(got, s) })
	sink("[2026-09-26 10:04] Hello")
	if len(got) != 1 || got[0] != "Hello" {
		t.Fatalf("single-chunk label: got %q, want [Hello]", got)
	}

	// Dump suppression still applies, before the gate and after it.
	got = nil
	sink = stampFilterStream(func(s string) { got = append(got, s) })
	sink("tool_call: read_file")
	sink("[Reading file...]")
	if len(got) != 0 {
		t.Fatalf("dumps must stay filtered, got %q", got)
	}
	// …including dump text riding behind a stripped label.
	sink("[2026-09-26 10:04] [Reading file...]")
	if len(got) != 0 {
		t.Fatalf("label + dump must stay filtered, got %q", got)
	}

	// Whitespace must not settle the gate (it used to be filtered
	// before ever reaching it): a label arriving after it still strips.
	got = nil
	sink = stampFilterStream(func(s string) { got = append(got, s) })
	sink(" ")
	sink("[2026-09-26 15:59] Set")
	if len(got) != 1 || got[0] != "Set" {
		t.Fatalf("label after whitespace: got %q, want [Set]", got)
	}

	// Normal text passes untouched.
	got = nil
	sink = stampFilterStream(func(s string) { got = append(got, s) })
	sink("Here is the forecast.")
	if len(got) != 1 || got[0] != "Here is the forecast." {
		t.Fatalf("normal text: got %q", got)
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
