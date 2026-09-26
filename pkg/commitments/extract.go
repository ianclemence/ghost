package commitments

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Extraction turns one owner message into zero or more durable obligations.
//
// Two rules govern everything here:
//
//  1. Only genuine obligations qualify. A speculation ("I might send Alex the
//     photos"), a hypothetical ("maybe I should..."), a question, a request to
//     Ghost ("can you send..."), and an explicit reminder request ("remind me
//     to send...") are all rejected — the reminder case because the scheduler
//     already owns it, the rest because they are not promises.
//
//  2. Nothing is invented. The obligation carries the owner's own words as its
//     provenance, the time comes from the runtime's existing natural-language
//     parser (never from model arithmetic), and the model path must return a
//     verbatim quote and time phrase that the message actually contains.
//
// The deterministic pass handles the overwhelmingly common shapes without any
// model call. The model pass exists for phrasing the patterns miss, and its
// output is verified against the message before it is believed.

// Candidate is a possible obligation derived from one message, before the
// runtime decides to persist it.
type Candidate struct {
	Text       string
	Subject    string
	Kind       Kind
	DuePhrase  string
	DueAt      *time.Time
	Quote      string
	Confidence float64
	Origin     string
}

// markerRE matches the phrases with which an owner commits to doing something.
var markerRE = regexp.MustCompile(`(?i)\b(i(?:'ve| have)? (?:still )?(?:need|have|got|must|want|intend|plan)\s+to|i(?:'m| am) going to|i(?:'ll| will)|i(?:'ve| have) been meaning to|i should)\b`)

// speculativere matches language that turns a statement into a maybe. A
// speculation is not an obligation, so these reject the whole message.
var speculativeRE = regexp.MustCompile(`(?i)\b(maybe|might|may\b|perhaps|possibly|probably not|not sure|unsure|thinking (?:about|of)|toying with|wondering|suppose|hypothetical|if i\b|should i\b|consider(?:ing)?\b|do i need)\b`)

// requestRE matches the owner asking Ghost to do it, which is a request, not a
// commitment. Explicit reminder requests are excluded too: the scheduler owns
// those, and storing them twice would create two durable records of one intent.
var requestRE = regexp.MustCompile(`(?i)^\s*(?:can|could|would|will) you\b|\bplease\b|\bremind me\b|\bset (?:a|an)? ?reminder\b|\bschedule\b`)

// futureIntentRE is the cheap gate in front of the semantic extractor. A
// message with no future-facing signal cannot be a promise, so the model is
// never asked about it — extraction must not add a model call to every turn.
var futureIntentRE = regexp.MustCompile(`(?i)\b(i(?:'ve| have)?\s+(?:still\s+)?(?:need|have|got|gotta|must|want|intend|plan|ought|should)|i(?:'m| am)\s+going\s+to|i(?:'ll| will)|meaning\s+to|going\s+to|planning\s+to|supposed\s+to|have\s+to|need\s+to)\b`)

// PossibleCommitment reports whether a message is worth asking a model about.
// It is deliberately looser than the deterministic marker: its job is to keep
// the model out of ordinary conversation, not to decide the outcome.
func PossibleCommitment(message string) bool {
	text := strings.TrimSpace(message)
	if text == "" || strings.HasPrefix(text, "/") {
		return false
	}
	if len(text) < 10 || strings.HasSuffix(text, "?") {
		return false
	}
	if speculativeRE.MatchString(text) || requestRE.MatchString(text) {
		return false
	}
	return futureIntentRE.MatchString(text)
}

// duePhraseRE finds the time expression the owner attached. The matched text is
// handed to the runtime's own scheduler parser; this regex only locates it.
var duePhraseRE = regexp.MustCompile(`(?i)\b(?:by|on|before|due|this|next|in)\s+([a-z]+(?:day|week|month)|\d{1,2}(?::\d{2})?\s*(?:am|pm)?)|(?i)\b(today|tonight|tomorrow|tonite|midnight|noon)\b|(?i)\b(mon|tues|wednes|thurs|fri|satur|sun)day\b|\b(\d{1,2}(?::\d{2})?\s*(?:am|pm))\b`)

// weekdayWords maps the weekday names the parser understands.
var weekdayWords = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday,
	"friday": time.Friday, "saturday": time.Saturday,
}

// kindFor derives the obligation shape from the verb the owner used. Unknown
// verbs are Other, which the runtime treats as "a personal task".
func kindFor(clause string) Kind {
	lower := strings.ToLower(clause)
	for _, k := range []struct {
		word string
		kind Kind
	}{
		{"send", KindSend}, {"email", KindEmail}, {"e-mail", KindEmail},
		{"call", KindCall}, {"ring", KindCall}, {"phone", KindCall},
		{"buy", KindBuy}, {"order", KindBuy}, {"pick up", KindBuy},
		{"pay", KindPay}, {"transfer", KindPay},
		{"book", KindBook}, {"reserve", KindBook}, {"schedule", KindBook},
		{"write", KindWrite}, {"draft", KindWrite}, {"finish", KindWrite},
		{"submit", KindSubmit}, {"file", KindSubmit}, {"upload", KindSubmit},
	} {
		if strings.Contains(lower, k.word) {
			return k.kind
		}
	}
	return KindOther
}

// namedSubject picks out a person or thing the owner named, using the one
// signal a message carries without a model: a capitalized word that is not the
// start of the sentence, a weekday, a month, or a common filler. When nothing
// qualifies, the subject is empty and the runtime will not pretend to know it.
func namedSubject(message string) string {
	skip := map[string]bool{
		"i": true, "im": true, "i'm": true, "ill": true, "i'll": true,
		"the": true, "a": true, "an": true, "my": true, "our": true,
		"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
		"friday": true, "saturday": true, "sunday": true,
		"january": true, "february": true, "march": true, "april": true,
		"may": true, "june": true, "july": true, "august": true,
		"september": true, "october": true, "november": true, "december": true,
		"today": true, "tomorrow": true, "tonight": true, "ghost": true,
	}
	fields := strings.Fields(message)
	for i, f := range fields {
		if i == 0 {
			continue // sentence start is capitalized for grammar, not for names
		}
		w := strings.Trim(f, ".,!?;:'\"()")
		if len(w) < 2 || skip[strings.ToLower(w)] {
			continue
		}
		if w[0] >= 'A' && w[0] <= 'Z' {
			return w
		}
	}
	return ""
}

// ResolveDue resolves a time phrase from the owner's words into an instant.
// It uses the scheduler's existing natural-language parser first, so an
// obligation and a reminder interpret time identically. Only when that parser
// needs a clock time the owner did not give does a small documented
// convention table apply — a named day means that day arriving, not an
// invented hour.
func ResolveDue(phrase string, now time.Time, timezone string) (*time.Time, bool) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return nil, false
	}
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	now = now.In(loc)

	// The runtime's own parser is authoritative when it can resolve.
	if p, err := scheduled.ParseNaturalLanguage(phrase, now, loc.String()); err == nil && p != nil {
		if p.IsOneTime && p.Schedule.At != nil {
			at := p.Schedule.At.In(loc)
			return &at, true
		}
	}

	lower := strings.ToLower(phrase)
	at := func(t time.Time) (*time.Time, bool) { return &t, true }

	switch {
	case strings.Contains(lower, "tonight") || strings.Contains(lower, "tonite"):
		t := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, loc)
		return at(t)
	case strings.Contains(lower, "today"):
		t := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc)
		return at(t)
	case strings.Contains(lower, "tomorrow"):
		d := now.AddDate(0, 0, 1)
		t := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
		return at(t)
	case strings.Contains(lower, "this weekend"):
		d := now
		for d.Weekday() != time.Saturday {
			d = d.AddDate(0, 0, 1)
		}
		t := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
		return at(t)
	case strings.Contains(lower, "next week"):
		d := now.AddDate(0, 0, 7)
		t := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
		return at(t)
	}
	// A bare weekday means that day arriving.
	for word, wd := range weekdayWords {
		if !strings.Contains(lower, word) {
			continue
		}
		d := now
		for {
			d = d.AddDate(0, 0, 1)
			if d.Weekday() == wd {
				break
			}
		}
		// "next friday" from Friday means the following week, not tomorrow.
		t := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
		return at(t)
	}
	return nil, false
}

// ExtractDeterministic finds obligations that need no semantic interpretation.
// It is the common path and it costs nothing.
func ExtractDeterministic(message string, now time.Time, timezone string) []Candidate {
	text := strings.Join(strings.Fields(message), " ")
	if text == "" || strings.HasSuffix(text, "?") {
		return nil
	}
	if speculativeRE.MatchString(text) || requestRE.MatchString(text) {
		return nil
	}
	loc := markerRE.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	clause := strings.TrimSpace(text[loc[1]:])
	// Stop at a sentence boundary: "… send the photos. Also I need to call Sam"
	// is two obligations, and the second clause will be found on its own when
	// the message is processed again for it.
	if idx := strings.IndexAny(clause, ".;!?"); idx > 0 {
		clause = strings.TrimSpace(clause[:idx])
	}
	clause = strings.TrimSpace(strings.TrimPrefix(clause, "to "))
	if len(clause) < 4 {
		return nil
	}
	c := Candidate{
		Text:       clause,
		Subject:    namedSubject(text),
		Kind:       kindFor(clause),
		Quote:      strings.TrimSpace(text[loc[0]:]),
		Confidence: 0.9,
		Origin:     "deterministic",
	}
	if raw := duePhraseRE.FindString(clause); raw != "" {
		if due, ok := ResolveDue(raw, now, timezone); ok {
			c.DuePhrase = strings.TrimSpace(raw)
			c.DueAt = due
		}
	}
	return []Candidate{c}
}

// ---------------------------------------------------------------------------
// Grounded semantic extraction
// ---------------------------------------------------------------------------

// ExtractionSystemPrompt asks for one strict JSON object. It states the
// negative cases explicitly, because the model's only job here is to notice
// phrasing the patterns miss — never to decide what the owner meant.
const ExtractionSystemPrompt = `You extract ONE durable personal obligation from a single message the user wrote.

A durable obligation is something the USER says THEY intend to do later.
Strong (extract): "I need to send Alex the photos Friday", "I have to renew my passport", "I'm going to call the bank tomorrow".
Explicit reminder requests (do NOT extract): "remind me to send Alex the photos", "set a reminder".
Weak intentions (do NOT extract): "I might send Alex the photos", "maybe I should send them".
Hypotheticals and questions (do NOT extract): "maybe I should...", "should I send these?", "if I sent these...".
Requests to you (do NOT extract): "can you send these", "please send these".

Rules:
- "quote" must be copied VERBATIM from the message. Never paraphrase it. Never invent it.
- "text" is the obligation in the user's own words, without the leading "I need to".
- "subject" is a person or thing the user named, copied verbatim, or "".
- "kind" is one of: send, email, call, buy, pay, book, write, submit, other.
- "time_expression" must be copied VERBATIM from the message, or "". Never compute or invent a date.
- "confidence" is 0..1: how sure you are this is a real obligation.
- If the message contains no durable obligation, set "is_commitment" to false.

Respond with ONLY a single JSON object:
{"is_commitment":bool,"text":str,"subject":str,"kind":str,"time_expression":str,"quote":str,"confidence":number}`

// SemanticResponse is the model's raw answer.
type SemanticResponse struct {
	IsCommitment   bool    `json:"is_commitment"`
	Text           string  `json:"text"`
	Subject        string  `json:"subject"`
	Kind           string  `json:"kind"`
	TimeExpression string  `json:"time_expression"`
	Quote          string  `json:"quote"`
	Confidence     float64 `json:"confidence"`
}

// SemanticExtractor asks a model for obligations and verifies every field
// against the message before anything is believed.
type SemanticExtractor struct {
	provider providers.LLMProvider
	model    string
}

// NewSemanticExtractor builds an extractor. A nil provider disables it.
func NewSemanticExtractor(p providers.LLMProvider, model string) *SemanticExtractor {
	if p == nil {
		return nil
	}
	if model == "" {
		model = p.GetDefaultModel()
	}
	return &SemanticExtractor{provider: p, model: model}
}

// Extract returns at most one verified obligation.
func (se *SemanticExtractor) Extract(ctx context.Context, message string, now time.Time, timezone string) (Candidate, bool) {
	if se == nil || se.provider == nil {
		return Candidate{}, false
	}
	if !PossibleCommitment(message) {
		return Candidate{}, false
	}
	text := strings.Join(strings.Fields(message), " ")
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := se.provider.Chat(callCtx, []providers.Message{
		{Role: "system", Content: ExtractionSystemPrompt},
		{Role: "user", Content: text},
	}, nil, se.model, map[string]interface{}{"temperature": 0.0, "max_tokens": 400})
	if err != nil || resp == nil {
		return Candidate{}, false
	}
	return VerifySemantic(resp.Content, text, now, timezone)
}

// VerifySemantic is the grounding gate. Every field the model returned must
// exist in the owner's message, or the whole extraction is discarded.
func VerifySemantic(raw, message string, now time.Time, timezone string) (Candidate, bool) {
	body := strings.TrimSpace(raw)
	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end <= start {
		return Candidate{}, false
	}
	var sr SemanticResponse
	if err := json.Unmarshal([]byte(body[start:end+1]), &sr); err != nil {
		return Candidate{}, false
	}
	if !sr.IsCommitment {
		return Candidate{}, false
	}
	if sr.Confidence < 0.7 {
		return Candidate{}, false
	}
	quote := strings.TrimSpace(sr.Quote)
	if quote == "" || !containsFold(message, quote) {
		return Candidate{}, false
	}
	// The obligation may not be broader than the words that justified it.
	if speculativeRE.MatchString(quote) || requestRE.MatchString(quote) {
		return Candidate{}, false
	}
	obligation := strings.TrimSpace(sr.Text)
	if obligation == "" {
		obligation = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(quote), "i need to"))
	}
	if len(obligation) < 4 {
		return Candidate{}, false
	}
	subject := strings.TrimSpace(sr.Subject)
	if subject != "" && !containsFold(message, subject) {
		// An invented person is exactly the failure this gate exists for.
		subject = ""
	}
	kind := Kind(strings.ToLower(strings.TrimSpace(sr.Kind)))
	if !ValidKind(kind) {
		kind = KindOther
	}
	c := Candidate{
		Text: obligation, Subject: subject, Kind: kind,
		Quote: quote, Confidence: sr.Confidence, Origin: "model",
	}
	// The time is resolved by the runtime, from a phrase the message really
	// contains — never from the model's arithmetic.
	expr := strings.TrimSpace(sr.TimeExpression)
	if expr != "" && containsFold(message, expr) {
		if due, ok := ResolveDue(expr, now, timezone); ok {
			c.DuePhrase = expr
			c.DueAt = due
		}
	}
	if c.DuePhrase == "" {
		// The model may have quoted the expression inside a longer phrase.
		if raw := duePhraseRE.FindString(quote); raw != "" {
			if due, ok := ResolveDue(raw, now, timezone); ok {
				c.DuePhrase = strings.TrimSpace(raw)
				c.DueAt = due
			}
		}
	}
	return c, true
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(
		strings.ToLower(strings.Join(strings.Fields(haystack), " ")),
		strings.ToLower(strings.Join(strings.Fields(needle), " ")),
	)
}

// Describe renders one obligation for a log line or a diagnostic. It is never
// the owner-facing text: the proposal renderer owns that.
func (c Candidate) Describe() string {
	due := "no time"
	if c.DueAt != nil {
		due = c.DueAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("%s (%s, due %s)", c.Text, c.Kind, due)
}
