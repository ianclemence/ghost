package agent

import (
	"unicode/utf8"
)

// narrationHold defers an iteration's prose just long enough to learn whether
// it is the answer or the sentence a model says before it goes and gets
// something ("I don't have a live news feed wired up for this, so let me
// search", "I'll check what's happening in Bangkok right now").
//
// The distinction only exists at the end of an iteration: the provider streams
// content first and tool calls arrive last, so nothing during the stream says
// which one this is. Holding until the iteration settles would mean the final
// answer could not stream at all — so the hold is bounded. A short preamble is
// still buffered when the tool calls land and is then discarded; anything that
// grows past narrationFlushAt is released and streams live from there on.
//
// Discarding is safe because such text was never part of the reply: the stored
// assistant message is built from the answer iteration, so a live transcript
// that carried the preamble was the odd one out. Content is only ever dropped
// from an iteration that went on to call tools; every other iteration is
// flushed in full.
type narrationHold struct {
	buf      []byte
	released bool
	gate     bool
	emit     func(string)
}

// narrationFlushAt bounds how much of an iteration may be held back. It is
// comfortably longer than a sentence of preamble and far shorter than a real
// answer, so a reply streams essentially at once while a "let me search" line
// never reaches the owner.
const narrationFlushAt = 160

func newNarrationHold(emit func(string)) *narrationHold {
	return &narrationHold{emit: emit}
}

// feed buffers until the hold is decisive, then passes everything through.
func (h *narrationHold) feed(chunk string) {
	if h == nil || chunk == "" {
		return
	}
	if h.released {
		h.emit(chunk)
		return
	}
	h.buf = append(h.buf, chunk...)
	if h.gate {
		// Gated turns hold the WHOLE iteration: once the turn has run a
		// consequential action, completion language must not reach the owner
		// until the runtime has checked it against evidence. The caller
		// emits the gated final content instead (see runLLMIteration).
		return
	}
	if utf8.RuneCountInString(string(h.buf)) >= narrationFlushAt {
		h.released = true
		out := string(h.buf)
		h.buf = nil
		h.emit(out)
	}
}

// flush releases whatever was held. Call it when the iteration produced no
// tool calls: the prose was the answer after all. A gated hold never emits
// here — the caller runs the evidence gate and emits the checked content.
func (h *narrationHold) flush() {
	if h == nil || h.released || len(h.buf) == 0 {
		return
	}
	if h.gate {
		return
	}
	out := string(h.buf)
	h.buf = nil
	h.emit(out)
}

// discard drops whatever was held. Call it when the iteration went on to call
// tools: the prose was a throat-clear between tool calls, and the owner should
// never have seen it.
func (h *narrationHold) discard() {
	if h == nil {
		return
	}
	h.buf = nil
}

// attemptRestartNotice marks, in the live stream only, that text the owner
// already saw belonged to a model attempt that failed and the answer is
// starting over. The stored reply is built from the successful attempt and
// never carries it.
const attemptRestartNotice = "\n\n(Interrupted — starting the answer again.)\n\n"

// abandonAttempt handles a model attempt that streamed text and then
// failed. Held text is dropped (the owner never saw it); if text was
// already released, the restart is marked so the next attempt's answer is
// not read as a continuation of the broken one.
func (h *narrationHold) abandonAttempt() {
	if h == nil {
		return
	}
	if !h.released || h.gate {
		h.buf = nil
		return
	}
	h.emit(attemptRestartNotice)
}
