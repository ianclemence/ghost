// Package sting is Ghost's offline tool-router plane.
//
// Sting is Ghost's own intent router: a small tool-calling model behind a
// loopback sidecar turns natural language into structured tool calls with
// ~28MB RAM. It is NOT a chat brain: off-topic input returns no call, and
// it never generates prose. Ghost uses it as an offline fast-path BEFORE
// the LLM:
//
//	user text → Sting sidecar → proposed calls → confidence gate +
//	strict grounding validation → Permission Broker (allow/ask/deny) →
//	Capability execution → runtime evidence → events → answer.
//
// Escalation (below confidence threshold, negation, missing slots,
// ungrounded values, sidecar down) falls through to the normal
// provider path (Ollama local / DeepSeek cloud). The model proposes,
// Ghost decides, executes, verifies, and records — never the reverse.
//
// Origin: the sidecar protocol and model contract are fork-compatible
// with Needle by Cactus Compute, Inc. (Apache-2.0; see
// THIRD-PARTY-NOTICES.md). Sting is an independent implementation under
// Ghost's MIT license — "Needle" is their trademark, not ours.
//
// This package is stdlib-only and imports neither providers nor tools
// so pkg/providers can adapt it without an import cycle.
package sting
