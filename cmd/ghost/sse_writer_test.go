package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The streaming handler serves one response from several goroutines (turn
// frames, keep-alive, clarify forwarder). net/http's ResponseWriter is not
// safe for concurrent use: unsynchronized writes interleave bytes at the
// HTTP chunk boundary and a strict client reader aborts mid-stream with
// "chunked line ends with bare LF" — the response the owner must never
// see. Every frame must route through sseWriter and arrive intact.
func TestSSEWriterSerializesConcurrentWriters(t *testing.T) {
	const frames = 20000
	var seq atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sw := &sseWriter{w: w, flush: w.(http.Flusher)}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		// Keep-alive goroutine, as in /v1/chat (tight here so the test
		// exercises the contention deterministically).
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					sw.keepalive()
				}
			}
		}()
		// Clarify-forwarder-shaped second writer.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sw.event(map[string]interface{}{"type": "lifecycle", "state": i})
			}
		}()
		// Main turn goroutine: token frames.
		for i := 0; i < frames; i++ {
			sw.event(fmt.Sprintf("frame-%04d-padding-padding-padding", seq.Add(1)))
		}
		close(stop)
		wg.Wait()
		sw.finish()
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("client transport error: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	last := 0
	sawDone := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var s string
		if err := json.Unmarshal([]byte(payload), &s); err != nil {
			continue // non-token frames carry objects
		}
		var n int
		if _, err := fmt.Sscanf(s, "frame-%d", &n); err != nil {
			continue
		}
		if n <= last {
			t.Fatalf("frame %d arrived after %d: concurrent writes corrupted the stream", n, last)
		}
		last = n
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("stream interrupted mid-response: %v", err)
	}
	if !sawDone {
		t.Fatalf("stream ended without [DONE] (last frame %d/%d)", last, frames)
	}
	if last != frames {
		t.Fatalf("lost frames: got %d/%d", last, frames)
	}
}

// After finish() writes [DONE] the response belongs to no one: a
// straggling goroutine (an in-flight keep-alive that raced the turn's
// end) must not inject frames past the terminator.
func TestSSEWriterFinishDropsLaterFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &sseWriter{w: rec, flush: rec}
	sw.event("before")
	sw.finish()
	sw.event("after")
	sw.keepalive()
	body := rec.Body.String()
	if !strings.Contains(body, "before") {
		t.Errorf("frames before finish() were dropped:\n%s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("finish() must write the [DONE] terminator:\n%s", body)
	}
	for _, late := range []string{"after", "keepalive"} {
		if strings.Contains(body, late) {
			t.Errorf("frame %q leaked past [DONE]:\n%s", late, body)
		}
	}
}
