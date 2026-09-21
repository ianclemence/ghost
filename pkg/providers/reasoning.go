// Ghost - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package providers

import "strings"

// Reasoning isolation.
//
// Ghost never surfaces a model's chain-of-thought. Providers expose reasoning
// on dedicated delta fields and/or inline tags; both are identified here and
// discarded, so no caller (agent loop, TUI, SSE bridge, mobile app) can render
// it. Thinking stays off by default elsewhere; this is the hard guarantee that
// holds even when a caller opts a reasoning model in.
//
// reasoningFieldNames is the allowlist of message/delta fields that carry
// model reasoning across OpenAI-compatible servers (llama.cpp, DeepSeek,
// Moonshot, NVIDIA, and others). None of these is answer text.
var reasoningFieldNames = []string{"reasoning_content", "reasoning", "reasoning_text"}

// inlineReasoningTags are the tag pairs some models emit directly inside the
// text stream. Content between them is reasoning, not answer text.
var inlineReasoningTags = [][2]string{
	{"<thinking>", "</thinking>"},
	{"<think>", "</think>"},
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// reasoningFieldValue returns the first non-empty reasoning field on a decoded
// message or delta, or "" when there is none. The value is expected to be
// stored separately (never concatenated into answer content).
func reasoningFieldValue(m map[string]interface{}) string {
	for _, f := range reasoningFieldNames {
		if v, ok := m[f].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// reasoningStreamFilter removes inline reasoning tags from a token stream while
// preserving ordinary text, even when a tag is split across chunks. It is the
// single place inline chain-of-thought is removed from streamed content.
type reasoningStreamFilter struct {
	inThink bool
	hold    string
}

// Write consumes one content chunk and returns the visible text safe to emit.
func (f *reasoningStreamFilter) Write(chunk string) string {
	f.hold += chunk
	var out strings.Builder
	for len(f.hold) > 0 {
		if f.inThink {
			idx, close := earliestReasoningTag(f.hold, false)
			if idx < 0 {
				// Retain only a possible partial closing tag; drop the rest.
				f.hold = reasoningTagSuffix(f.hold, false)
				return out.String()
			}
			f.hold = f.hold[idx+len(close):]
			f.inThink = false
			continue
		}
		idx, open := earliestReasoningTag(f.hold, true)
		if idx < 0 {
			keep := reasoningTagSuffix(f.hold, true)
			out.WriteString(f.hold[:len(f.hold)-len(keep)])
			f.hold = keep
			return out.String()
		}
		out.WriteString(f.hold[:idx])
		f.hold = f.hold[idx+len(open):]
		f.inThink = true
	}
	return out.String()
}

// Flush releases any buffered non-reasoning text at end of stream. A dangling
// unclosed reasoning tag is dropped (it was reasoning).
func (f *reasoningStreamFilter) Flush() string {
	out := ""
	if !f.inThink && len(f.hold) > 0 && !isPartialReasoningTag(f.hold, true) {
		out = f.hold
	}
	f.hold = ""
	return out
}

// stripInlineReasoning removes inline reasoning tags from a complete string.
func stripInlineReasoning(s string) string {
	f := &reasoningStreamFilter{}
	return f.Write(s) + f.Flush()
}

// earliestReasoningTag returns the index and tag of the first opening
// (open=true) or closing (open=false) reasoning tag in s, or -1 if none.
func earliestReasoningTag(s string, open bool) (int, string) {
	best := -1
	bestTag := ""
	for _, pair := range inlineReasoningTags {
		tag := pair[1]
		if open {
			tag = pair[0]
		}
		if i := strings.Index(s, tag); i >= 0 && (best < 0 || i < best) {
			best, bestTag = i, tag
		}
	}
	return best, bestTag
}

// reasoningTagSuffix returns the longest suffix of s that is a proper prefix of
// any opening (open=true) or closing tag, so it can be held for the next chunk.
func reasoningTagSuffix(s string, open bool) string {
	best := ""
	for _, pair := range inlineReasoningTags {
		tag := pair[1]
		if open {
			tag = pair[0]
		}
		max := len(tag) - 1
		if len(s) < max {
			max = len(s)
		}
		for n := max; n > 0; n-- {
			if strings.HasSuffix(s, tag[:n]) && n > len(best) {
				best = tag[:n]
			}
		}
	}
	return best
}

// isPartialReasoningTag reports whether s is a proper prefix of an opening tag.
func isPartialReasoningTag(s string, open bool) bool {
	for _, pair := range inlineReasoningTags {
		tag := pair[1]
		if open {
			tag = pair[0]
		}
		if len(s) < len(tag) && strings.HasPrefix(tag, s) {
			return true
		}
	}
	return false
}
