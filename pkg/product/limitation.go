package product

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limitation policy: when does a limitation belong in the owner's answer?
//
// Ghost's first instinct was to disclose everything it knew about how it got an
// answer — a page that would not parse, a snippet instead of an article body, a
// publication whose pages came back empty. Every disclosure was true, and
// together they made Ghost sound defensive and uncertain about everything,
// which is its own kind of dishonesty: it teaches the owner to discount
// statements that are actually solid.
//
// The runtime knows a great deal about the shape of its own evidence
// (source_type, source_access, evidence_strength, verification_state). That
// knowledge is valuable for audit, replay and evaluation. It is not
// automatically valuable to the owner. Knowing a limitation and needing to
// announce it are different things.
//
// This file is the single policy that separates them. It is deterministic and
// lives in the package that already owns Ghost's LLM-independent user-facing
// language, so honesty here does not depend on a model remembering an
// instruction.

// Limitation is how much a limitation deserves to reach the owner's eyes.
type Limitation string

const (
	// LimitationNone: true about the retrieval path, irrelevant to the answer.
	// Recorded internally, never spoken. A page that would not parse while two
	// other sources corroborate the same facts ends here.
	LimitationNone Limitation = "none"
	// LimitationContextual: worth one natural clause woven into the answer, or
	// a slight lowering of the wording's confidence. Not a labelled block, and
	// not a separate sentence explaining retrieval.
	LimitationContextual Limitation = "contextual"
	// LimitationMaterial: the owner should be told, because omitting it would
	// mislead them about what is known.
	LimitationMaterial Limitation = "material"
	// LimitationBlocking: the thing could not be done or read at all. There is
	// no answer to give, only the honest statement of why.
	LimitationBlocking Limitation = "blocking"
)

// Surfaces reports whether a limitation may appear in user-facing prose.
func (l Limitation) Surfaces() bool { return l != LimitationNone }

// LimitationFacts are the structured facts a limitation decision may use. They
// are deliberately about the owner's request and the evidence obtained — never
// about internal retrieval mechanics, which cannot by themselves justify an
// interruption.
type LimitationFacts struct {
	// RequestedSource is a publication or page the owner explicitly asked for,
	// e.g. "the Bangkok Post". Empty when they just asked a question.
	RequestedSource string
	// RequestedSourceRead reports whether that source's own content was read.
	RequestedSourceRead bool
	// AnyEvidence reports whether any usable evidence was obtained at all.
	AnyEvidence bool
	// SnippetOnly reports that the usable evidence was listings, headlines or
	// snippets rather than read-through content.
	SnippetOnly bool
	// Corroborated reports that at least two independent sources support the
	// material facts.
	Corroborated bool
	// Conflicting reports that sources disagree about a material fact.
	Conflicting bool
	// UnverifiedAction reports that an action ran but its post-condition could
	// not be confirmed.
	UnverifiedAction bool
	// FailedAction reports that an action the owner asked for did not happen.
	FailedAction bool
}

// DecideLimitation applies the policy. It is deterministic and order-sensitive:
// the first condition that holds wins, from most to least severe.
//
// The rule it embodies: a caveat is earned by the answer's reliability, not by
// the retrieval's imperfection.
func DecideLimitation(f LimitationFacts) Limitation {
	switch {
	case f.FailedAction:
		// The owner asked for something and it did not happen. That is the
		// whole message.
		return LimitationBlocking
	case !f.AnyEvidence:
		// Nothing usable. Say so; do not dress it up.
		return LimitationBlocking
	case f.Conflicting:
		// Sources disagree. The owner would be misled by a confident
		// single-line answer.
		return LimitationMaterial
	case f.RequestedSource != "" && !f.RequestedSourceRead:
		// They asked for a specific source and Ghost could not read it. This
		// is the one case where "I couldn't access X" is the answer's content,
		// not a footnote.
		return LimitationMaterial
	case f.UnverifiedAction:
		// The action was dispatched but the outcome is not confirmed. Natural
		// for the owner to assume success, so it has to be said.
		return LimitationMaterial
	case f.SnippetOnly && !f.Corroborated:
		// Headline-level evidence standing alone. The wording should be more
		// careful, but the owner does not need a lecture about snippets.
		return LimitationContextual
	default:
		// Everything else — a page that would not parse, one source among
		// several, a body Ghost chose not to read in full — is the runtime's
		// business, not the owner's.
		return LimitationNone
	}
}

// CaveatRule is the prompt-facing statement of the same policy. GHOST.md must
// carry it verbatim; a test asserts that, so the prose the model follows and
// the decision the runtime makes cannot drift apart.
const CaveatRule = "Be honest about uncertainty and never pretend something was verified that was not. " +
	"Do not narrate how you retrieved something, and do not add a caveat merely because a page would not " +
	"parse, a result was a snippet, or one source was unavailable. Surface a limitation when leaving it out " +
	"would mislead: the owner asked for a source you could not read, sources disagree, the evidence is too " +
	"thin to carry the claim, or an action ran but its outcome is unconfirmed. Calibrate by attribution and " +
	"wording — \"Reuters reports\", \"early reports indicate\" — not by disclaimer."

// closingOfferStartRE matches a sentence that is only an offer of further
// work. Deliberately narrow: it must open with the offer itself, so a sentence
// that happens to contain "want me to" mid-way is left alone. The conditional
// opener is held to the same standard — "If you want, I can…" is an offer,
// "If you want the raw data, it's below" is content and must never be eaten.
var closingOfferStartRE = regexp.MustCompile(`(?i)^(?:want me to|would you like me to|i can also|i could also|say the word|let me know if you|happy to|shall i|should i)\b|^if you (?:want|like|prefer)(?:,|[[:space:]]+(?:i|i'?d|i'?ll|me)\b)`)

// closingOfferClauseRE matches an offer welded onto the end of a sentence that
// is otherwise content: "…the eastern side of the city is the worst hit —
// want me to check the current rain and AQI for your area?" The lead is a real
// observation and stays; only the offered work goes. End-anchored,
// dash-introduced and short, so it can only ever take a trailing courtesy.
var closingOfferClauseRE = regexp.MustCompile(`(?i)[[:space:]]+(?:—|–|-)[[:space:]]+(?:want me to|would you like me to|shall i|should i|i can also|i could also|let me know if|happy to|say the word)[^?\n]{0,160}\?[[:space:]]*$`)

// closingOfferMaxRunes bounds what may be dropped: a closing courtesy, never a
// paragraph.
const closingOfferMaxRunes = 200

// TrimClosingOffer removes a purely conversational closing offer from the end
// of a finished reply ("Want me to pull the full article?").
//
// The prompt already tells the model not to end with an engagement question.
// Small models do it anyway — observed repeatedly on information answers — and
// the runtime has the one thing the model lacks: it knows whether anything
// failed. So the rule is enforced here, narrowly and deterministically:
//
//   - only the FINAL sentence is a candidate, and only when the reply has more
//     than one sentence (a bare offer is the whole reply and is left alone);
//   - the candidate must begin with an offer, and be short — or end with one
//     attached after a dash, in which case only the attached clause goes;
//   - if any tool failed, nothing is trimmed: "Want me to retry?" after a
//     failure is material and must survive.
//
// This is not a place to hide uncertainty. It removes a question, never a fact,
// and any qualification the model wrote stays exactly where it was.
func TrimClosingOffer(reply string, toolFailed bool) string {
	if toolFailed || reply == "" {
		return reply
	}
	trimmed := strings.TrimRight(reply, " \t\r\n")
	if trimmed == "" {
		return reply
	}
	// A model that offers twice gets both removed, but only ever from the end
	// and only offer-shaped — bounded so this can never eat prose.
	for i := 0; i < 3; i++ {
		if cut, ok := cutOfferClause(trimmed); ok {
			trimmed = cut
			continue
		}
		start := lastSentenceStart(trimmed)
		if start <= 0 {
			return trimmed // a single sentence: leave it alone
		}
		last := strings.TrimSpace(trimmed[start:])
		if last == "" || utf8.RuneCountInString(last) > closingOfferMaxRunes {
			return trimmed
		}
		if !closingOfferStartRE.MatchString(last) {
			return trimmed
		}
		kept := strings.TrimRight(trimmed[:start], " \t\r\n")
		if strings.TrimSpace(kept) == "" {
			return reply // never trim the whole reply away
		}
		trimmed = kept
	}
	return trimmed
}

// cutOfferClause removes a trailing "…content — offer?" tail, keeping the
// content clause and giving it back its full stop. ok=false means there is
// nothing offer-shaped to remove and the reply is returned untouched.
func cutOfferClause(s string) (string, bool) {
	loc := closingOfferClauseRE.FindStringIndex(s)
	if loc == nil {
		return s, false
	}
	if utf8.RuneCountInString(s[loc[0]:]) > closingOfferMaxRunes {
		return s, false
	}
	kept := strings.TrimRight(s[:loc[0]], " \t\r\n")
	if strings.TrimSpace(kept) == "" {
		return s, false // never trim the whole reply away
	}
	// The sentence that carried the offer was cut short, so finish it.
	if r, _ := utf8.DecodeLastRuneInString(kept); unicode.IsLetter(r) || unicode.IsDigit(r) {
		kept += "."
	}
	return kept, true
}

// labelledCaveatStartRE matches the disclaimer frame itself: a paragraph that
// opens by announcing that a caveat follows. Group 1 is the label, so a cut can
// start exactly there and leave the sentence before it alone.
var labelledCaveatStartRE = regexp.MustCompile(`(?i)(?:^|[.!?\n]\s*|\s)((?:one caveat|caveat|sourcing note|note on sources|please note(?: that)?|for transparency|to be transparent|note that)\s*:)`)

// caveatSourceShapeRE names the thing that makes a caveat about shape rather
// than substance: how the retrieved material was cut, not what it says. This is
// `source_access` — internal metadata the task keeps inside Ghost — showing up
// in prose.
var caveatSourceShapeRE = regexp.MustCompile(`(?i)\b(homepages?|front pages?|headlines?|listings?|snippets?|summaries|summari[sz]ed|previews?|excerpts?|abstracts?|above the fold|listing text)\b`)

// caveatMaterialRE marks a caveat that carries something the owner needs. A
// paragraph containing any of these is never touched: it names what went wrong,
// how thin the evidence is, or that sources cannot both be right.
var caveatMaterialRE = regexp.MustCompile(`(?i)\b(couldn'?t|could not|unable|not able|failed|failure|denied|blocked|inaccessible|unavailable|no access|cannot|can't|can’t|disagree|conflicting|conflict|disputed|contradictory|unconfirmed|not confirmed|unverified|not verified|uncorroborated|unsourced|no source|single|sole|only one|one outlet|one source|no other|unreachable|didn't load|didn’t load|wouldn't load|wouldn’t load|no readable|not read|requested|paywall|behind a subscription|missing|unclear|alleged|rumou?rs?|only\s+(?:one|two|three|the|a))\b`)

// labelledCaveatMaxRunes bounds the block, and is deliberately no larger than
// the stream's tail window (closingOfferHoldMax): a block that would not fit
// the window cannot be cut on both paths, and history and transcript must
// agree.
const labelledCaveatMaxRunes = 200

// TrimLabelledCaveat removes a trailing paragraph that opens with the
// disclaimer frame the prompt bans — the sentence this whole change exists to
// stop: "One caveat: these come from The Star and Kenyans.co.ke homepages, so
// it's a snapshot of what they're covering."
//
// A prompt rule was not enough: the model still produced it on roughly one
// news answer in fifteen. The runtime is the layer that can decide this
// correctly, because it is the only one that knows whether anything failed.
//
// Every one of these conditions must hold, and each is there to protect honesty
// rather than to catch phrases:
//
//   - nothing failed this turn — after a failure a caveat is earned;
//   - the label opens the FINAL block, so a note in the middle of an answer is
//     left where it is;
//   - it is short — a paragraph is not a courtesy;
//   - it talks about the shape of what was retrieved (homepages, headlines,
//     listings), which is internal detail, never about what went wrong;
//   - it contains nothing the owner needs: no inability, no conflict, no
//     unverified outcome, no single source;
//   - removing it does not empty the reply.
//
// A material limitation fails at least one of those and survives untouched.
func TrimLabelledCaveat(reply string, toolFailed bool) string {
	if toolFailed || reply == "" {
		return reply
	}
	trimmed := strings.TrimRight(reply, " \t\r\n")
	if trimmed == "" {
		return reply
	}
	// The last label that starts a block with nothing but that block after it.
	var labelAt []int
	for _, m := range labelledCaveatStartRE.FindAllStringSubmatchIndex(trimmed, -1) {
		if m[1] < 0 {
			continue
		}
		if strings.Contains(trimmed[m[1]:], "\n\n") {
			continue // something real follows it; not the trailing block
		}
		labelAt = m
	}
	if labelAt == nil {
		return reply
	}
	start := labelAt[2]
	after := trimmed[start:]
	if utf8.RuneCountInString(after) > labelledCaveatMaxRunes {
		return reply
	}
	if !caveatSourceShapeRE.MatchString(after) {
		return reply // not about how it was retrieved: leave it
	}
	if caveatMaterialRE.MatchString(after) {
		return reply // carries something the owner needs
	}
	kept := strings.TrimRight(trimmed[:start], " \t\r\n")
	if strings.TrimSpace(kept) == "" {
		return reply // never trim the whole reply away
	}
	if r, _ := utf8.DecodeLastRuneInString(kept); unicode.IsLetter(r) || unicode.IsDigit(r) {
		kept += "."
	}
	return kept
}

// lastSentenceStart returns the index where the final sentence begins, or 0
// when there is only one sentence.
func lastSentenceStart(s string) int {
	start := 0
	for i := 0; i+1 < len(s); i++ {
		switch s[i] {
		case '.', '!', '?', '\n':
			if s[i+1] == ' ' || s[i+1] == '\t' || s[i+1] == '\n' {
				start = i + 1
			}
		}
	}
	return start
}
