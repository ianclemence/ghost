package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// sseWriter owns every byte a streaming handler writes to one response.
//
// A /v1/chat turn serves its response from several goroutines: the turn
// goroutine (tokens, tool events, lifecycle frames, [DONE]), the
// keep-alive ticker, and the clarify forwarder. net/http's ResponseWriter
// is not safe for concurrent use — it writes through a bare bufio.Writer
// with no lock — so unsynchronized frames interleave bytes at the HTTP
// chunk boundary and the client's chunked reader aborts mid-stream with
// "chunked line ends with bare LF" (the response must never surface to
// the owner). Every frame therefore goes through this one mutex, and
// frames are dropped after finish() so a straggling goroutine can never
// write past [DONE].
type sseWriter struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	flush  http.Flusher
	closed bool
}

// frame writes one raw SSE payload as a data frame (payload + blank
// separator). Frames written after finish() are dropped.
func (s *sseWriter) frame(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.writeLocked("data: " + body + "\n\n")
}

// writeLocked emits one already-framed payload. Callers hold s.mu.
func (s *sseWriter) writeLocked(body string) {
	fmt.Fprint(s.w, body)
	s.flush.Flush()
}

// event JSON-encodes payload into a data frame.
func (s *sseWriter) event(payload interface{}) {
	raw, _ := json.Marshal(payload)
	s.frame(string(raw))
}

// keepalive writes a comment frame that keeps clients and intermediaries
// from timing out a long turn.
func (s *sseWriter) keepalive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.writeLocked(": keepalive\n\n")
}

// finish writes the terminal [DONE] frame and seals the writer inside one
// critical section, so no frame can slip in behind the terminator.
func (s *sseWriter) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.writeLocked("data: [DONE]\n\n")
	s.closed = true
}

// seal closes the writer without a frame: a straggling goroutine that
// outlives the handler gets a no-op instead of a write into a response
// the server already finished.
func (s *sseWriter) seal() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}
