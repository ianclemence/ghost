package main

import (
	"strings"
	"testing"
)

func drain(ch <-chan liveFrame, n int) []liveFrame {
	var out []liveFrame
	for i := 0; i < n; i++ {
		select {
		case f, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, f)
		default:
			return out
		}
	}
	return out
}

func TestLiveTurnIsFollowedFromStartToEnd(t *testing.T) {
	h := newTurnHub()
	_, frames, stop := h.Subscribe()
	defer stop()
	h.Begin("main", "r1", "cli", "hello")
	h.Delta("main", "r1", "Hi ")
	h.Tool("main", "r1", "Searching the web")
	h.Delta("main", "r1", "there")
	h.End("main", "r1", "success")
	got := drain(frames, 10)
	var types []string
	text := ""
	for _, f := range got {
		types = append(types, f.Type)
		text += f.Delta
	}
	if strings.Join(types, ",") != "stream_start,stream_delta,stream_tool,stream_delta,stream_end" || text != "Hi there" {
		t.Fatalf("frames %v text %q", types, text)
	}
	if got[len(got)-1].Text != "Hi there" || got[0].UserText != "hello" || got[0].Origin != "cli" {
		t.Fatalf("start/end must carry the question, origin and whole reply: %+v", got)
	}
}

// A surface that opens the app halfway through a reply sees what was written
// so far, then the rest, with nothing missed and nothing repeated.
func TestLateViewerGetsASnapshotThenTheRest(t *testing.T) {
	h := newTurnHub()
	h.Begin("main", "r1", "cli", "explain boot")
	h.Delta("main", "r1", "First part. ")
	snap, frames, stop := h.Subscribe()
	defer stop()
	if len(snap) != 1 || snap[0].Type != "stream_snapshot" || snap[0].Text != "First part. " || snap[0].UserText != "explain boot" {
		t.Fatalf("snapshot: %+v", snap)
	}
	h.Delta("main", "r1", "Second part.")
	h.End("main", "r1", "success")
	got := drain(frames, 5)
	if len(got) != 2 || got[0].Delta != "Second part." || got[1].Text != "First part. Second part." {
		t.Fatalf("the rest must follow the snapshot exactly once: %+v", got)
	}
	if len(h.Snapshot()) != 0 {
		t.Fatal("a finished reply is no longer in progress")
	}
}

func TestStaleAndUnknownTurnsAreIgnored(t *testing.T) {
	h := newTurnHub()
	_, frames, stop := h.Subscribe()
	defer stop()
	h.Delta("main", "ghost", "x") // nothing running
	h.Begin("main", "r2", "mobile", "q")
	h.Delta("main", "old-request", "y") // wrong request
	h.End("main", "old-request", "success")
	if got := drain(frames, 10); len(got) != 1 || got[0].Type != "stream_start" {
		t.Fatalf("only the running request counts: %+v", got)
	}
}

// A viewer that cannot keep up is dropped, not shown a garbled reply.
func TestSlowViewerIsDroppedNotGarbled(t *testing.T) {
	h := newTurnHub()
	_, frames, stop := h.Subscribe()
	defer stop()
	h.Begin("main", "r1", "cli", "q")
	for i := 0; i < liveSubBuffer+10; i++ {
		h.Delta("main", "r1", "x")
	}
	closed := false
	for i := 0; i < liveSubBuffer+20; i++ {
		if _, ok := <-frames; !ok {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("a viewer that fell behind must be disconnected so it reconnects to a fresh snapshot")
	}
}
