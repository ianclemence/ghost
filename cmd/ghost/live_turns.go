package main

import (
	"strings"
	"sync"
	"time"
)

// Live turns: how a reply can be watched from more than one place.
//
// The reply to a message is produced on the Pod whether or not anyone is
// looking. The device that sent the message reads it over its own request; the
// hub lets every other surface follow the same reply as it is written. A
// surface that connects halfway through (the app opened after a message was
// sent from the terminal) is handed what has been written so far, then the
// rest live, and when the reply ends it is told, so it can swap the live text
// for the saved message. This is the same shape ChatGPT and Claude use: the
// server owns the generation, and each client is only a viewer of it.

// liveFrame is one event about a running reply.
type liveFrame struct {
	Type      string `json:"type"` // stream_snapshot | stream_start | stream_delta | stream_tool | stream_end
	Session   string `json:"session_id"`
	RequestID string `json:"request_id"`
	Origin    string `json:"origin,omitempty"`       // the surface that sent the message
	UserText  string `json:"user_content,omitempty"` // what was asked (start and snapshot)
	Delta     string `json:"delta,omitempty"`        // new text (stream_delta)
	Text      string `json:"text,omitempty"`         // all text so far (snapshot) or the whole reply (end)
	Tool      string `json:"tool,omitempty"`         // what Ghost is doing right now
	Outcome   string `json:"outcome,omitempty"`      // stream_end: success | failed | waiting
}

type liveTurn struct {
	requestID string
	origin    string
	userText  string
	text      strings.Builder
	tool      string
	started   time.Time
}

type liveSub struct {
	ch chan liveFrame
}

type turnHub struct {
	mu     sync.Mutex
	active map[string]*liveTurn // by session
	subs   map[int]*liveSub
	next   int
}

func newTurnHub() *turnHub {
	return &turnHub{active: map[string]*liveTurn{}, subs: map[int]*liveSub{}}
}

// turns is the daemon's hub.
var turns = newTurnHub()

// checkpointFlush writes the reply written so far to durable storage so a
// restart can put it back. It is a hook rather than a direct dependency:
// the hub is process-global and constructed before the durable turn log is
// opened, and a turn logged without one still streams normally.
var checkpointFlush func(session, requestID, text string, force bool)

const liveSubBuffer = 512

// broadcast sends f to every subscriber. A subscriber that cannot keep up is
// dropped rather than made to miss part of a reply and show garbled text; its
// connection closes, it reconnects, and it is handed a fresh snapshot.
func (h *turnHub) broadcast(f liveFrame) {
	for id, s := range h.subs {
		select {
		case s.ch <- f:
		default:
			close(s.ch)
			delete(h.subs, id)
		}
	}
}

// Begin records that a reply is starting.
func (h *turnHub) Begin(session, requestID, origin, userText string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active[session] = &liveTurn{requestID: requestID, origin: origin, userText: userText, started: time.Now()}
	h.broadcast(liveFrame{Type: "stream_start", Session: session, RequestID: requestID, Origin: origin, UserText: userText})
}

// Delta appends text to the running reply.
func (h *turnHub) Delta(session, requestID, text string) {
	if text == "" {
		return
	}
	h.mu.Lock()
	t := h.active[session]
	if t == nil || t.requestID != requestID {
		h.mu.Unlock()
		return
	}
	t.text.WriteString(text)
	// Clone under the lock: Builder.String shares its buffer, so the copy
	// handed to the durability write must not alias text the next token
	// appends to.
	soFar := strings.Clone(t.text.String())
	h.broadcast(liveFrame{Type: "stream_delta", Session: session, RequestID: requestID, Delta: text})
	h.mu.Unlock()
	// Outside the lock: the durability write must not hold up the reply or
	// the other surfaces watching it. It is throttled downstream, so this
	// is a cheap call per token, not a write per token.
	if checkpointFlush != nil {
		checkpointFlush(session, requestID, soFar, false)
	}
}

// Tool notes what Ghost is doing (searching, reading…) for a status line.
func (h *turnHub) Tool(session, requestID, label string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.active[session]
	if t == nil || t.requestID != requestID {
		return
	}
	t.tool = label
	h.broadcast(liveFrame{Type: "stream_tool", Session: session, RequestID: requestID, Tool: label})
}

// End records that a reply is over. text is the whole reply, so a surface can
// show it immediately without waiting for history to catch up.
func (h *turnHub) End(session, requestID, outcome string) {
	h.mu.Lock()
	t := h.active[session]
	if t == nil || t.requestID != requestID {
		h.mu.Unlock()
		return
	}
	delete(h.active, session)
	soFar := strings.Clone(t.text.String())
	h.broadcast(liveFrame{Type: "stream_end", Session: session, RequestID: requestID, Origin: t.origin, Text: soFar, Outcome: outcome})
	h.mu.Unlock()
	// Final flush, unthrottled. A turn that ends waiting on an approval
	// keeps its partial on disk; a turn that completed retires it, because
	// the reply is already a row in the transcript.
	if checkpointFlush != nil && soFar != "" {
		checkpointFlush(session, requestID, soFar, true)
	}
}

// Snapshot returns a frame for every reply in progress, for a surface that has
// just connected.
func (h *turnHub) Snapshot() []liveFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []liveFrame
	for session, t := range h.active {
		out = append(out, liveFrame{
			Type: "stream_snapshot", Session: session, RequestID: t.requestID, Origin: t.origin,
			UserText: t.userText, Text: t.text.String(), Tool: t.tool,
		})
	}
	return out
}

// Subscribe registers a viewer. It returns the frames and a function to stop.
// The snapshot is taken under the same lock as the registration, so nothing is
// missed or repeated between "what has been written" and "what comes next".
func (h *turnHub) Subscribe() (snapshot []liveFrame, frames <-chan liveFrame, stop func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &liveSub{ch: make(chan liveFrame, liveSubBuffer)}
	id := h.next
	h.next++
	h.subs[id] = s
	for session, t := range h.active {
		snapshot = append(snapshot, liveFrame{
			Type: "stream_snapshot", Session: session, RequestID: t.requestID, Origin: t.origin,
			UserText: t.userText, Text: t.text.String(), Tool: t.tool,
		})
	}
	return snapshot, s.ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if cur, ok := h.subs[id]; ok {
			close(cur.ch)
			delete(h.subs, id)
		}
	}
}
