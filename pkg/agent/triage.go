package agent

import (
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// Effort classifies how much work a turn deserves. Ghost should be fast for
// simple things and deliberate for complex ones — and honest when it doesn't
// know — rather than running every request through the same expensive loop.
type Effort int

const (
	// EffortDeliberate is the default: a full plan/act/observe loop. Preserves
	// current behavior so complex or ambiguous requests are never starved.
	EffortDeliberate Effort = iota
	// EffortFast: a simple fact already held in memory — answer directly,
	// no tool calls, no model.
	EffortFast
	// EffortUnknown: there is genuinely nothing to act on; be honest rather
	// than forcing the model to invent an answer.
	EffortUnknown
)

func (e Effort) String() string {
	switch e {
	case EffortFast:
		return "fast"
	case EffortUnknown:
		return "unknown"
	default:
		return "deliberate"
	}
}

// classifyEffort is a lightweight, mostly-deterministic triage. It is
// deliberately conservative: it only upgrades to a special tier for clear,
// unambiguous signals and otherwise returns the default (Deliberate). We never
// optimistically skip work for a request we're not sure about.
func classifyEffort(msg string) Effort {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return EffortUnknown
	}
	// Only the clear "ask me about my stored self-knowledge" family is Fast.
	// These are deterministic and safe to answer from Personal Context.
	for _, re := range fastSelfRegex {
		if re.MatchString(m) {
			return EffortFast
		}
	}
	return EffortDeliberate
}

// fastSelfRegex matches unambiguous self/identity fact-recall questions that can
// be answered directly from Personal Context without invoking the model or any
// tool. It deliberately stays narrow: only question phrasings that cannot also
// be a declaration (so a "my favorite color is blue" turn still flows through
// the extractor), and only the facts the memory/digest system isn't used to
// probe through the LLM path.
var fastSelfRegex = []*regexp.Regexp{
	// "what is my name" / "who am i" / "what's my name"
	regexp.MustCompile(`\bwhat(?:'s| is)? my name\b|\bwho am i\b`),
	// "where do i live" / "where am i"
	regexp.MustCompile(`\bwhere (do i live|am i)\b`),
	// "what is my email" / "what is my phone number"
	regexp.MustCompile(`\bwhat is my (email|phone)\b`),
	// "what do i prefer/like/enjoy" — clear questions, never declarations.
	regexp.MustCompile(`\bwhat do i (prefer|like|enjoy)\b`),
}

// predicateForFast maps an intent to the Personal Context predicate(s) that
// answer it, so the fast path looks up the exact belief rather than guessing.
func predicateForFast(m string) []string {
	switch {
	case regexp.MustCompile(`\b(who am i|what(?:'s| is)? my name)\b`).MatchString(m):
		return []string{"identity/name"}
	case regexp.MustCompile(`\bwhere (do i live|am i)\b`).MatchString(m):
		return []string{"fact/location", "identity/location"}
	case regexp.MustCompile(`\bwhat is my phone\b`).MatchString(m):
		return []string{"identity/phone"}
	case regexp.MustCompile(`\bwhat is my email\b`).MatchString(m):
		return []string{"identity/email"}
	case regexp.MustCompile(`\bwhat do i (prefer|like|enjoy)\b`).MatchString(m):
		return []string{"preference/prefers", "preference/likes"}
	case regexp.MustCompile(`\bwhat(?:'s| is) my (favorite|favourite)\b`).MatchString(m):
		return []string{"preference/likes", "preference/prefers"}
	}
	return nil
}

// fastPathAnswer answers a Fast-effort request deterministically from Personal
// Context. It returns (answer, handled). When the belief is stored it answers
// precisely; when it isn't, it is honest ("I don't have that yet") rather than
// inventing one. If this isn't a fast-path request, it reports not-handled so
// the normal loop runs.
func (al *AgentLoop) fastPathAnswer(m, session string) (string, bool) {
	if al.pcStore == nil {
		return "", false
	}
	preds := predicateForFast(strings.ToLower(m))
	if len(preds) == 0 {
		return "", false
	}
	// Collect the current values for the relevant predicates, scoped to
	// the session's context (cross-context facts never answer here).
	var values []string
	for _, e := range al.pcStore.CurrentInScope(al.sessionScopes(session)) {
		for _, p := range preds {
			if e.Predicate == p {
				if v := personalcontext.Value(e); v != "" {
					values = append(values, v)
				}
			}
		}
	}
	if len(values) == 0 {
		// Honest, non-fabricated response — the exact opposite of a model
		// inventing an answer.
		return "I don\u2019t have that stored yet. Tell me and I\u2019ll remember it.", true
	}
	// Phrase the stored value as an answer to the question that was asked.
	// The stored form is a third-person note ("The user's name is Maya",
	// "Lives in Bangkok"); returning it verbatim reads like a database row,
	// not a companion answering you.
	return phraseFastAnswer(preds[0], values), true
}

// phraseFastAnswer renders a stored belief as a natural reply. The stored
// value stays the source of truth; only the delivery changes. Unknown
// predicates fall back to the plain value so behavior never regresses.
func phraseFastAnswer(predicate string, values []string) string {
	first := stripThirdPersonPrefix(values[0])
	switch predicate {
	case "identity/name":
		return "You're " + first + "."
	case "identity/location", "fact/location":
		return "You live in " + first + "."
	case "identity/phone":
		return "Your phone number is " + first + "."
	case "identity/email":
		return "Your email is " + first + "."
	case "preference/likes", "preference/prefers":
		if len(values) == 1 {
			return "You like " + first + "."
		}
		return "You like " + strings.Join(values, ", ") + "."
	}
	if len(values) == 1 {
		return first
	}
	return strings.Join(values, " and ")
}

// stripThirdPersonPrefix removes a leading "The user's X is" / "Lives in"
// wrapper the extractor sometimes stores, so the answer can be re-voiced.
func stripThirdPersonPrefix(v string) string {
	s := strings.TrimSpace(v)
	for _, marker := range []string{"name is ", "is named ", "lives in ", "works as ", "works in "} {
		if idx := strings.Index(strings.ToLower(s), marker); idx >= 0 {
			return strings.TrimSpace(s[idx+len(marker):])
		}
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "the user") || strings.HasPrefix(lower, "user's") || strings.HasPrefix(lower, "user is") {
		if idx := strings.Index(lower, " is "); idx >= 0 {
			return strings.TrimSpace(s[idx+4:])
		}
	}
	return s
}
