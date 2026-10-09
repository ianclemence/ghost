package watch

import (
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/commitments"
)

// Detection turns one owner message into zero or more watch candidates.
// It is deterministic: patterns over the owner's own words, a time phrase
// resolved by the runtime's existing scheduler parser, and guards that
// reject everything a watch must not be.
//
// Two rules govern everything here:
//
//  1. A watch exists only for something in the world that will change on
//     its own and that the owner's words place in the future. A past event
//     is history, an undated one is a guess.
//
//  2. Nothing is invented. The entity is copied from the message, the time
//     comes from the runtime parser (never model arithmetic), and the
//     provenance quote is a verbatim span of what the owner wrote.
//
// The guards are deliberately conservative — Ghost would rather miss a
// watch than watch the wrong person's flight, or fork a reminder into a
// second durable record the owner never asked for.

// Candidate is a possible watch derived from one message, before the
// runtime's policy decides to persist it.
type Candidate struct {
	Kind        Kind
	Entity      string
	Label       string
	EventAt     *time.Time
	EventPhrase string
	Quote       string
	Confidence  float64
	Origin      string // deterministic | explicit
	Explicit    bool   // the owner asked for this watch by name
	// URL and Rule are a page watch's page and what the owner wants from it.
	URL  string
	Rule *PageRule
}

// travelRE is the context a bare flight number needs before Ghost may read
// it as travel. Without it, "BA123" in any other sentence stays a string.
var travelRE = regexp.MustCompile(`(?i)\b(?:flight|flights|fly|flying|flew|boarding|board|depart(?:ing|ure)?|arriv(?:ing|al)|plane|airplane|airport|airline|takeoff|landing|airfare)\b`)

// flightNumberRE matches an IATA-style flight number (two letters, one to
// four digits). Case-sensitive on purpose: lowercase words must never be
// read as airline codes.
var flightNumberRE = regexp.MustCompile(`\b([A-Z]{2})\s?(\d{1,4})\b`)

// explicitRE matches the owner asking Ghost to watch something by name.
var explicitRE = regexp.MustCompile(`(?i)\b(?:track|watch|follow|monitor|keep\s+(?:me\s+)?(?:an\s+)?eye\s+on|stay\s+on\s+top\s+of|check\s+up\s+on|let\s+me\s+know\s+(?:about|if|when))\b`)

// reminderGuardRE rejects turns the scheduler already owns. A reminder must
// not also become a watch: one stated intent, one durable record.
var reminderGuardRE = regexp.MustCompile(`(?i)\b(?:remind\s+me|set\s+(?:a|an)?\s*reminder|remember\s+(?:that\s+)?|note\s+that)\b`)

// routineGuardRE rejects turns the routine engine already owns: a repeated
// cadence is not an observation of one external event. It keys on explicit
// recurrence only — "on Thursday" is a DATE the owner stated, while "on
// Thursdays" is a cadence, and conflating them would refuse every dated
// appointment.
var routineGuardRE = regexp.MustCompile(`(?i)\b(?:every|each|daily|weekly|biweekly|monthly|yearly|annually|hourly|weekdays?|weekends?)\b|\bon\s+(?:mondays|tuesdays|wednesdays|thursdays|fridays|saturdays|sundays)\b`)

// thirdPersonRE matches language about somebody else. A watch is the
// owner's observation of their own affair: a friend's flight is not
// Ghost's to interrupt them about.
var thirdPersonRE = regexp.MustCompile(`(?i)\b(?:his|her|their)\b|\bmy\s+(?:friend|brother|sister|mom|mum|mother|dad|father|wife|husband|partner|spouse|colleague|boss|client|son|daughter|kid|child|children|parents?|roommate|neighbor|neighbour|cousin|uncle|aunt|grandma|grandfather|team)'s\b`)

// appointmentRE matches the shapes of an appointment the owner has.
var appointmentRE = regexp.MustCompile(`(?i)\b(?:appointment|check-?up|dentist|doctor|physio(?:therapist)?|consultation|therapy|interview|eye\s+exam|physical|scan|procedure)\b`)

// reservationRE matches a booking the owner holds.
var reservationRE = regexp.MustCompile(`(?i)\b(?:reservation|booking|table\s+(?:at|for)|room\s+at|hotel|airbnb|ticket(?:s)?\s+for|show\s+tickets)\b`)

// deliveryRE matches a shipment heading to the owner.
var deliveryRE = regexp.MustCompile(`(?i)\b(?:package|parcel|delivery|deliveries|shipment|shipped|out\s+for\s+delivery|tracking\s+number|order\s+(?:is|was)\s+(?:on\s+the\s+way|shipped|coming))\b`)

// eventRE matches a dated occurrence the owner will attend.
var eventRE = regexp.MustCompile(`(?i)\b(?:concert|game|match|flight\s+home|wedding|conference|show|festival|ceremony|graduation)\b`)

// eventPhraseRE locates the time expression the owner attached. It only
// finds the span; commitments.ResolveDue (the runtime's own parser) does
// the interpreting, so a watch and a reminder read "Friday" identically.
var eventPhraseRE = regexp.MustCompile(`(?i)\b(?:by|on|before|due|this|next|in|at)?\s*\b(?:today|tonight|tonite|tomorrow|midnight|noon|(?:mon|tues|wednes|thurs|fri|satur|sun)day|\d{1,2}(?::\d{2})?\s*(?:am|pm))\b`)

// Detect returns every watch candidate the message honestly supports.
func Detect(message string, now time.Time, timezone string) []Candidate {
	text := strings.Join(strings.Fields(message), " ")
	if text == "" || strings.HasPrefix(text, "/") {
		return nil
	}
	lower := strings.ToLower(text)
	if reminderGuardRE.MatchString(lower) || routineGuardRE.MatchString(lower) {
		return nil
	}
	explicit := explicitRE.MatchString(text)
	// A link in an explicit request is a page to watch, and only that: the
	// words around it say what about the page matters, not a second watch.
	if explicit && !thirdPersonRE.MatchString(text) {
		if c, ok := detectPage(text); ok {
			return []Candidate{c}
		}
	}

	var out []Candidate
	seen := map[string]bool{}
	add := func(c Candidate) {
		if c.Kind == "" || c.Entity == "" {
			return
		}
		key := string(c.Kind) + "|" + normalize(c.Entity)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, c)
	}

	// Flights: a code plus travel context (or an explicit request) plus a
	// future date.
	if m := flightNumberRE.FindStringSubmatchIndex(text); m != nil {
		code := text[m[2]:m[3]] + text[m[4]:m[5]]
		code = strings.ToUpper(strings.ReplaceAll(code, " ", ""))
		sentence := sentenceAround(text, m[0])
		ctxOK := travelRE.MatchString(sentence) || explicit
		if ctxOK && !thirdPersonBefore(text, m[0]) && !thirdPersonRE.MatchString(sentence) {
			c := Candidate{
				Kind: KindFlight, Entity: code, Label: "Flight " + code,
				Quote: sentence, Confidence: 0.9, Origin: "deterministic", Explicit: explicit,
			}
			if phrase, at, ok := findEventTime(sentence, now, timezone); ok {
				c.EventAt, c.EventPhrase = at, phrase
			}
			// A future event is required for an automatic watch; an
			// explicit request may stand without one (the cadence has a
			// documented no-event default).
			if c.EventAt != nil || explicit {
				add(c)
			}
		}
	}

	// Phrased kinds: appointment, reservation, delivery, event.
	for _, pat := range []struct {
		re   *regexp.Regexp
		kind Kind
	}{
		{appointmentRE, KindAppointment},
		{reservationRE, KindReservation},
		{deliveryRE, KindDelivery},
		{eventRE, KindEvent},
	} {
		loc := pat.re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		sentence := sentenceAround(text, loc[0])
		if thirdPersonBefore(text, loc[0]) || thirdPersonRE.MatchString(sentence) {
			continue
		}
		// The phrase itself must be in a sentence that also carries a
		// future time: an appointment with no date is not yet watchable.
		phrase, at, ok := findEventTime(sentence, now, timezone)
		if !ok || at == nil {
			if !explicit {
				continue
			}
		}
		phraseText := strings.TrimSpace(text[loc[0]:loc[1]])
		c := Candidate{
			Kind: pat.kind, Entity: strings.ToLower(phraseText), Label: phraseText,
			Quote: sentence, Confidence: 0.9, Origin: "deterministic", Explicit: explicit,
			EventAt: at, EventPhrase: phrase,
		}
		if c.EventAt == nil && !explicit {
			continue
		}
		add(c)
	}
	return out
}

// findEventTime locates and resolves the time expression in a sentence.
// It returns ok=false when the sentence carries no resolvable time at all.
func findEventTime(sentence string, now time.Time, timezone string) (string, *time.Time, bool) {
	m := eventPhraseRE.FindString(sentence)
	if strings.TrimSpace(m) == "" {
		return "", nil, false
	}
	at, ok := commitments.ResolveDue(strings.TrimSpace(m), now, timezone)
	if !ok || at == nil {
		return "", nil, false
	}
	return strings.TrimSpace(m), at, true
}

// thirdPersonBefore reports whether a third-person marker appears between
// the start of the message and the entity, i.e. the entity belongs to
// somebody else.
func thirdPersonBefore(text string, idx int) bool {
	loc := thirdPersonRE.FindStringIndex(text)
	return loc != nil && loc[0] <= idx
}

// sentenceAround returns the verbatim sentence containing idx, bounded so
// provenance stays a quote rather than a paragraph. It never edits the
// words — only the span.
func sentenceAround(text string, idx int) string {
	if idx > len(text) {
		idx = len(text)
	}
	start := 0
	for i := idx; i > 0; i-- {
		if strings.ContainsRune(".;!?\n", rune(text[i-1])) {
			start = i
			break
		}
	}
	end := len(text)
	for i := idx; i < len(text); i++ {
		if strings.ContainsRune(".;!?\n", rune(text[i])) {
			end = i + 1
			break
		}
	}
	q := strings.TrimSpace(text[start:end])
	if len(q) > 240 {
		if idx-start > 160 {
			start = idx - 80
		}
		q = strings.TrimSpace(text[start:min(start+240, len(text))])
	}
	return q
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Describe renders a candidate for a log line. Never owner-facing.
func (c Candidate) Describe() string {
	when := "no time"
	if c.EventAt != nil {
		when = c.EventAt.Format(time.RFC3339)
	}
	return string(c.Kind) + " " + c.Entity + " (" + when + ")"
}

// ToWatch converts a candidate into the record the store will hold. The
// source is NOT set here: it is resolved by the registry at creation time,
// so a watch never claims a source that was not consulted.
func (c Candidate) ToWatch(session, msgID string, now time.Time) Watch {
	w := Watch{
		Kind: c.Kind, Entity: strings.TrimSpace(c.Entity), Label: c.Label,
		EventAt: c.EventAt, Status: StatusActive, Explicit: c.Explicit,
		URL: c.URL, Rule: c.Rule,
		Provenance: Provenance{Session: session, MessageID: msgID, Quote: c.Quote, At: now},
		CreatedAt:  now, UpdatedAt: now,
	}
	if c.Kind == KindPage {
		// A page has no event to end on: it is watched for a month, then
		// Ghost says it stopped.
		end := now.Add(PageHorizon)
		w.ExpiresAt = &end
	}
	return w
}
