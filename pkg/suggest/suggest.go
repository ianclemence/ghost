// Package suggest predicts what the owner is most likely to type next, so the
// empty message box can offer it instead of a fixed line.
//
// Two sources, in order. Rules read how Ghost's last message ended (an offer,
// a list, a report) and answer it directly: instant, free, and always the same
// for the same message. When no rule applies, a small model call is asked for
// one short line, and whatever it returns is checked hard (Clean) before it is
// ever shown. A suggestion is only ever text for the owner to accept or ignore:
// nothing here sends anything.
package suggest

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxLen is the longest suggestion, in characters. It must fit one row of the
// phone's message bar beside its "Use" button, which is the narrowest place.
const MaxLen = 24

// Turn is one message of the recent conversation.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

var (
	// "Want me to …?", "Shall I …?" and the like: Ghost offering to do something.
	offerRE = regexp.MustCompile(`(?i)\b(?:do you want me to|would you like me to|want me to|shall i|should i)\s+([^?.\n]{2,160})\?`)
	// A question that begins with a wh-word asks for information ("Which city
	// should I check?"), not for a yes.
	whRE        = regexp.MustCompile(`(?i)^\W*(?:which|what|when|where|who|whom|whose|how|why)\b`)
	pendingRE   = regexp.MustCompile(`(?m)^\d+ pending:`)
	weatherRE   = regexp.MustCompile(`^Weather in [^:\n]+:`)
	markdownRE  = regexp.MustCompile("[*_`#>]+")
	youRE       = regexp.MustCompile(`(?i)\byou\b`)
	yourRE      = regexp.MustCompile(`(?i)\byour\b`)
	prefixRE    = regexp.MustCompile(`(?i)^(?:you|user|me|human)\s*:\s*`)
	spacesRE    = regexp.MustCompile(`\s+`)
	sentenceEnd = regexp.MustCompile(`[.!?\n]`)
	grantRE     = regexp.MustCompile(`(?i)\b(?:approve|approved|allow|always allow|deny|denied)\b`)
)

// FromRules returns the suggestion that follows directly from how Ghost's last
// message ended, or false when nothing does.
func FromRules(last string) (string, bool) {
	text := strings.TrimSpace(markdownRE.ReplaceAllString(last, ""))
	if text == "" {
		return "", false
	}
	// The offer wins: it is the thing Ghost is waiting on.
	if ms := offerRE.FindAllStringSubmatchIndex(text, -1); len(ms) > 0 {
		m := ms[len(ms)-1]
		before := text[:m[0]]
		if i := lastSentenceStart(before); i >= 0 {
			before = before[i:]
		}
		if !whRE.MatchString(before) {
			return yesTo(text[m[2]:m[3]]), true
		}
		return "", false
	}
	switch {
	case pendingRE.MatchString(text):
		return "Move the first one", true
	case weatherRE.MatchString(text):
		return "What about tomorrow?", true
	}
	return "", false
}

// lastSentenceStart returns the index just after the last sentence break in s.
func lastSentenceStart(s string) int {
	locs := sentenceEnd.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return -1
	}
	return locs[len(locs)-1][1]
}

// yesTo turns the first thing Ghost offered into the owner's answer: "move it,
// or add a nudge" becomes "Yes, move it", and "tell you if it changes" becomes
// "Yes, tell me if it changes". An offer too long to repeat becomes a plain yes.
func yesTo(offer string) string {
	phrase := strings.TrimSpace(offer)
	for _, cut := range []string{", or ", " or ", ",", ";"} {
		if i := strings.Index(strings.ToLower(phrase), cut); i > 0 {
			phrase = phrase[:i]
		}
	}
	phrase = strings.TrimSpace(strings.Trim(phrase, " ."))
	phrase = yourRE.ReplaceAllString(phrase, "my")
	phrase = youRE.ReplaceAllString(phrase, "me")
	phrase = spacesRE.ReplaceAllString(phrase, " ")
	if phrase == "" {
		return "Yes, go ahead"
	}
	out := "Yes, " + strings.ToLower(phrase[:1]) + phrase[1:]
	if utf8.RuneCountInString(out) > MaxLen {
		return "Yes, go ahead"
	}
	return out
}

// Clean checks a model's suggestion and returns it ready to show, or false.
// The model is asked for one short line; anything else (a refusal, an essay, a
// repeat of Ghost's own words, markup) is dropped rather than shown.
func Clean(s, lastAssistant string) (string, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = prefixRE.ReplaceAllString(s, "")
	s = strings.Trim(s, " \t\"'`*“”‘’")
	if s == "" || strings.EqualFold(strings.Trim(s, ". "), "none") {
		return "", false
	}
	n := utf8.RuneCountInString(s)
	if n < 2 || n > MaxLen {
		return "", false
	}
	if strings.ContainsAny(s, "{}<>[]") || strings.Contains(strings.ToLower(s), "http") {
		return "", false
	}
	if strings.EqualFold(s, strings.TrimSpace(lastAssistant)) {
		return "", false
	}
	// Granting or refusing permission is the owner's deliberate act, made on
	// the approval itself, never a one-tap suggestion in the message bar. A
	// "Yes, approve" offered after the task was already done went nowhere.
	if grantRE.MatchString(s) {
		return "", false
	}
	return s, true
}

// Prompt builds the one small request that asks for a suggestion from the
// recent conversation (oldest first). Each message is clipped so the call
// stays cheap however long the conversation is.
func Prompt(turns []Turn) (system, user string) {
	system = "You write one short suggestion for what a person would most likely type next in a chat with their personal AI assistant, " +
		"using the last few messages. Reply with ONLY that message, written as the person would type it: first person, at most six words, " +
		"under 24 characters, no quotes, no explanation. If the assistant just asked a question or made an offer, suggest the likely answer. " +
		"If nothing natural follows, reply NONE."
	var b strings.Builder
	for _, t := range turns {
		who := "Assistant"
		if t.Role == "user" {
			who = "Person"
		}
		txt := strings.TrimSpace(t.Text)
		if r := []rune(txt); len(r) > 400 {
			txt = string(r[:400]) + "…"
		}
		fmt.Fprintf(&b, "%s: %s\n", who, txt)
	}
	b.WriteString("\nNext message from Person:")
	return system, b.String()
}
