package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/utils"
)

// stampStream holds the leading bytes of one streamed reply until it can
// prove they are (or are not) the model imitating the internal history
// date label "[2006-01-02 15:04] …". The label is invisible bookkeeping —
// it exists only in the model's own context — and a model that sees it
// sometimes writes it back. Streaming is an output boundary, so the label
// must be caught here too: by the time text is on screen it is too late.
//
// The hold is tiny and start-only: the first chunk that cannot grow into
// a label flushes immediately (any reply not beginning with a digit or
// "[" waits nothing), and once the question is settled every later chunk
// passes through untouched.
type stampStream struct {
	buf     []byte
	settled bool // label resolved (or proven absent): pass everything through
}

// feed returns the bytes safe to emit now. ok=false means "hold" — the
// buffer may still be the start of a label; call again with the next
// chunk. An empty out with ok=true means everything held so far WAS the
// label and there is nothing to emit yet.
func (s *stampStream) feed(chunk string) (out string, ok bool) {
	if s.settled {
		return chunk, true
	}
	s.buf = append(s.buf, chunk...)
	candidate := string(s.buf)
	if utils.HistoryStampRe.MatchString(candidate) {
		// The label (possibly doubled by an eager model) is complete:
		// drop it, keep the content, and never second-guess again.
		s.settled = true
		s.buf = nil
		return utils.StripDateStamp(candidate), true
	}
	if utils.CouldStartHistoryStamp(candidate) {
		return "", false // still ambiguous — hold
	}
	s.settled = true
	s.buf = nil
	return candidate, true
}

// couldStartHistoryLabel reports whether text is (or could still grow
// into) the internal history date label — the one "["-shape that must
// reach the stamp gate instead of the dump filter.
func couldStartHistoryLabel(s string) bool {
	if s == "" {
		return false
	}
	return utils.HistoryStampRe.MatchString(s) || utils.CouldStartHistoryStamp(s)
}

// stampFilterStream is the model-chunk sink for one turn. The order is
// load-bearing: a chunk the dump filter would drop is still fed to the
// stamp gate when it could be the internal history label, and what the
// gate emits is filtered again — so every byte that reaches the
// transcript clears the dump filter, but nothing can eat the label
// before the gate sees it.
//
// The failure this fixes: the label opens with "[", and a model emits
// that bracket as its own chunk. Filtering first dropped it, the gate
// only ever saw the body ("2026-09-2610:04] …"), could not match a
// label without "[", and the invisible bookkeeping became a visible
// date fragment on the live transcript.
func stampFilterStream(inner func(string)) func(string) {
	ss := &stampStream{}
	return func(s string) {
		trimmed := strings.TrimSpace(s)
		// Internal dumps are suppressed before the gate, exactly as
		// before, so a dump chunk can never settle the gate's decision
		// about a label that has not finished arriving. The label
		// itself is the exception: it must get through to strip.
		if shouldFilterAssistantChunk(s) && !couldStartHistoryLabel(trimmed) {
			return
		}
		out, ok := ss.feed(s)
		if !ok || out == "" {
			return
		}
		if shouldFilterAssistantChunk(out) {
			return
		}
		inner(out)
	}
}
