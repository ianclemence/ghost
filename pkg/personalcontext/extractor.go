package personalcontext

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// Input is one user turn plus the context the extractor may inspect. The
// extractor is a pure function of this input: given the same Input it returns
// the same actions, and it never reads, writes, or deletes conversation
// evidence or requires a model.
type Input struct {
	// SessionID and MessageID name the message the extractor is running on and
	// become the provenance ref (session_id:message_id). Runtime messages do
	// not carry ids, so the caller supplies them.
	SessionID string
	MessageID string

	// Text is the user message to extract from.
	Text string

	// Timestamp is the message creation time. It becomes the source timestamp
	// and entry timestamps. A zero timestamp defaults to now (UTC) so an entry
	// is never written without one.
	Timestamp time.Time

	// PreviousText is the immediately preceding user turn. It is used only to
	// resolve deictic corrections like "actually, it's green", and only when it
	// resolves to exactly one unambiguous declaration.
	PreviousText string

	// Current is the current context the extractor may consult to decide
	// whether a candidate creates, supersedes, or restates an existing entry.
	// Callers typically pass Store.Current().
	Current []Entry
}

// ActionMode is how an extraction result should be persisted.
type ActionMode string

const (
	// ActionCreate appends a brand-new entry.
	ActionCreate ActionMode = "create"
	// ActionSupersede replaces the current entry for the same subject and
	// predicate, retiring the old one.
	ActionSupersede ActionMode = "supersede"
	// ActionReinforce records that an existing belief was restated, bumping its
	// reinforcement metadata without duplicating it (provenance preserved).
	ActionReinforce ActionMode = "reinforce"
)

// Action is one extraction result: a fully-built entry plus the mode a caller
// should use to persist it. Persistence itself stays in Store.
type Action struct {
	Mode ActionMode
	// Entry is complete (id, kind, subject, predicate, value, status,
	// confidence, sources) and ready to be handed to Store.Create or
	// Store.Supersede.
	Entry Entry
	// Rule names the grammar rule that produced the action, for diagnostics
	// and tests.
	Rule string
}

const (
	// entrySubjectUser is the subject every extracted entry is attached to.
	entrySubjectUser = "user"
	// declaredConfidence is used for explicit user declarations.
	declaredConfidence = 0.95
	// correctedConfidence is used for explicit user corrections.
	correctedConfidence = 1.0
	// likesRuleName is the name of the additive like rule. It identifies
	// additive actions during persistence re-resolution: likes never
	// supersede, while every other rule keeps one current entry per belief.
	likesRuleName = "likes"
)

// declarationRule is one grammar rule. Patterns are matched case-insensitively
// against the text; the captured value is sliced from the original text so the
// stored value keeps the user's casing ("Ian", "Bangkok").
type declarationRule struct {
	name      string
	kind      Kind
	predicate string
	re        *regexp.Regexp
	// stripNow trims a trailing "now" from the value (location corrections).
	stripNow bool
	// likes marks additive preferences: like-entries are never superseded, and
	// restatements of an existing like value are skipped.
	likes bool
	// prefix is prepended to the captured value before storage.
	prefix string
	// rejectWords skips a capture whose first word is in this list, so a more
	// specific rule (e.g. communication style) owns those declarations.
	rejectWords []string
}

// declarationRules is the entire extractor grammar. Values are captured
// bounded by clause punctuation (; . , ! ?) and then cut at the first " and "
// or " but " so a multi-clause message like "my name is Ian and I live in
// Bangkok" yields two clean values instead of one bleeding value.
var declarationRules = []declarationRule{
	{
		name:      "favorite_color",
		kind:      KindPreference,
		predicate: "preference/favorite_color",
		re:        regexp.MustCompile(`(?i)\bmy favorite colou?r is\s+([^;.,!?]+)`),
	},
	{
		name:      "name",
		kind:      KindIdentity,
		predicate: "identity/name",
		re:        regexp.MustCompile(`(?i)\bmy name is\s+([^;.,!?]+)`),
	},
	{
		name:      "location",
		kind:      KindFact,
		predicate: "fact/location",
		re:        regexp.MustCompile(`(?i)\bi live in\s+([^;.,!?]+)`),
		stripNow:  true,
	},
	{
		name:      "communication_style",
		kind:      KindPreference,
		predicate: "preference/communication.style",
		re:        regexp.MustCompile(`(?i)\bi prefer\s+(concise|brief|short|detailed|thorough|elaborate|direct|casual|formal|verbose)\s+answers?\b`),
	},
	{
		name:      likesRuleName,
		kind:      KindPreference,
		predicate: "preference/likes",
		re:        regexp.MustCompile(`(?i)\bi like\s+([^;.,!?]+)`),
		likes:     true,
	},
	{
		name:        "prefers",
		kind:        KindPreference,
		predicate:   "preference/prefers",
		re:          regexp.MustCompile(`(?i)\bprefers?\s+([^;.,!?]+)`),
		likes:       true,
		rejectWords: []string{"concise", "brief", "short", "detailed", "thorough", "elaborate", "direct", "casual", "formal", "verbose", "answers", "answer", "responses", "response"},
	},
	{
		name:      "favorite",
		kind:      KindPreference,
		predicate: "preference/favorite",
		re:        regexp.MustCompile(`(?i)\bmy favorite\s+(?:food|drink|show|movie|book|song|place|thing)\s+is\s+([^;.,!?]+)`),
		likes:     true,
	},
	{
		name:      "goal",
		kind:      KindGoal,
		predicate: "goal/primary",
		re:        regexp.MustCompile(`(?i)\bmy goal is to\s+([^;.,!?]+)`),
	},
	{
		name:      "want_build",
		kind:      KindGoal,
		predicate: "goal/primary",
		re:        regexp.MustCompile(`(?i)\bi want to build\s+([^;.,!?]+)`),
		prefix:    "build ",
	},
	{
		name:      "partner",
		kind:      KindRelationship,
		predicate: "relationship/partner",
		re:        regexp.MustCompile(`(?i)\bmy (?:wife|husband|partner|spouse|girlfriend|boyfriend)'?s? name is\s+([^;.,!?]+)`),
	},
	{
		name:      "work",
		kind:      KindFact,
		predicate: "fact/work",
		re:        regexp.MustCompile(`(?i)\bi work (?:as|at|for)\s+([^;.,!?]+)`),
	},
	{
		name:      "job",
		kind:      KindFact,
		predicate: "fact/job",
		re:        regexp.MustCompile(`(?i)\bmy job is\s+([^;.,!?]+)`),
	},
}

var (
	// correctionPrefixRE strips explicit correction lead-ins.
	correctionPrefixRE = regexp.MustCompile(`(?i)^(?:(?:actually|no|nah|nope|correction)(?:[,:]|\s)+|(?:that'?s|thats)\s+wrong(?:[,:]|\s)+)`)
	// rememberPrefixRE strips the explicit memory command language.
	rememberPrefixRE = regexp.MustCompile(`(?i)^remember\b[\s:]+(?:that\b[\s:]+)?`)
	// pronounRE matches deictic corrections like "it's green".
	pronounRE = regexp.MustCompile(`(?i)^(?:it'?s|it is)\s+(.+)`)
	// trailingNowRE trims a trailing "now" (location corrections).
	trailingNowRE = regexp.MustCompile(`(?i)\s+now\s*$`)
)

// likesStopwords guards the additive like rule against low-signal captures:
// "I like that", "I like it when you ...", "I like how ..." are not
// preferences.
var likesStopwords = map[string]struct{}{
	"about": {}, "and": {}, "because": {}, "but": {}, "how": {}, "if": {},
	"it": {}, "that": {}, "the": {}, "them": {}, "then": {}, "there": {},
	"this": {}, "to": {}, "what": {}, "when": {}, "whether": {}, "which": {},
	"with": {}, "you": {}, "your": {},
}

// candidate is one matched declaration awaiting lifecycle resolution.
type candidate struct {
	kind       Kind
	predicate  string
	value      string
	rule       string
	correction bool
	likes      bool
}

// Extract analyzes one user turn and returns the actions a caller should
// persist. It is pure: no store, no model, no side effects. Zero candidates
// is a valid, common result for ordinary conversation.
func Extract(in Input) ([]Action, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, nil
	}

	text, isCorrection := stripCorrection(text)
	text = stripRemember(text)

	cands := matchDeclarations(text, isCorrection)

	// Deictic correction: "actually, it's green". Only resolvable when the
	// immediately preceding user turn yields exactly one unambiguous
	// declaration; otherwise no candidate, rather than guessing.
	if len(cands) == 0 {
		if value, ok := matchPronoun(text); ok {
			if kind, predicate, rule, ok := resolveFromPrevious(in.PreviousText); ok {
				cands = append(cands, candidate{
					kind:       kind,
					predicate:  predicate,
					value:      value,
					rule:       "pronoun:" + rule,
					correction: true,
				})
			}
		}
	}

	return decideActions(cands, in), nil
}

// Apply is the thin persistence wrapper for the turn loop: it extracts and
// persists every action through the store. Persistence logic itself stays in
// Store; Apply only dispatches. Decisions are re-resolved against the live
// store under its lock (see Store.applyActions), so a correction whose entry
// disappeared between extraction and persistence is written as a new
// declaration, and a concurrent or same-message declaration of the same
// predicate supersedes instead of creating a duplicate current entry.
func Apply(store *Store, in Input) ([]Action, error) {
	if len(in.Current) == 0 {
		in.Current = store.Current()
	}
	actions, err := Extract(in)
	if err != nil {
		return nil, err
	}
	return store.applyActions(actions)
}

// stripCorrection removes a leading correction marker and reports whether one
// was present.
func stripCorrection(text string) (string, bool) {
	if loc := correctionPrefixRE.FindStringIndex(text); loc != nil {
		return text[loc[1]:], true
	}
	return text, false
}

// stripRemember removes the leading "remember that ..." / "remember: ..."
// command language. A remembered statement is a declaration, not a correction.
func stripRemember(text string) string {
	if loc := rememberPrefixRE.FindStringIndex(text); loc != nil {
		return text[loc[1]:]
	}
	return text
}

// matchDeclarations runs every grammar rule against the text and collects the
// matched candidates. Values are sliced from the original text (case
// preserved), clause-bounded, and cleaned before being returned.
func matchDeclarations(text string, correction bool) []candidate {
	var out []candidate
	for _, r := range declarationRules {
		m := r.re.FindStringSubmatchIndex(text)
		if m == nil || m[2] < 0 {
			continue
		}
		value := text[m[2]:m[3]]
		if r.stripNow {
			value = stripTrailingNow(value)
		}
		value = trimClause(value)
		value = cleanValue(value)
		if value == "" {
			continue
		}
		if r.likes && rejectLikesValue(value) {
			continue
		}
		if len(r.rejectWords) > 0 && rejectFirstWord(value, r.rejectWords) {
			continue
		}
		if r.prefix != "" {
			value = r.prefix + value
		}
		out = append(out, candidate{
			kind:       r.kind,
			predicate:  r.predicate,
			value:      value,
			rule:       r.name,
			correction: correction,
			likes:      r.likes,
		})
	}
	return out
}

// matchPronoun recognizes "it's X" / "it is X" after the correction and
// remember lead-ins have been stripped.
func matchPronoun(text string) (string, bool) {
	m := pronounRE.FindStringSubmatchIndex(text)
	if m == nil || m[2] < 0 {
		return "", false
	}
	value := cleanValue(trimClause(text[m[2]:m[3]]))
	if value == "" {
		return "", false
	}
	return value, true
}

// resolveFromPrevious runs the declaration grammar over the immediately
// preceding user turn. It resolves only when that turn yields exactly one
// declaration, so deictic corrections never guess.
func resolveFromPrevious(previous string) (Kind, string, string, bool) {
	text := strings.TrimSpace(previous)
	if text == "" {
		return "", "", "", false
	}
	stripped, _ := stripCorrection(text)
	stripped = stripRemember(stripped)
	cands := matchDeclarations(stripped, false)
	if len(cands) != 1 {
		return "", "", "", false
	}
	return cands[0].kind, cands[0].predicate, cands[0].rule, true
}

// decideActions resolves each candidate against the current context. A
// correction with no current entry becomes a new declaration (user_declared);
// a declaration or correction that restates the current value produces no
// action; a differing value supersedes the current entry.
func decideActions(cands []candidate, in Input) []Action {
	var actions []Action
	for _, c := range cands {
		cur := currentEntry(in.Current, c.predicate)

		if c.likes {
			if currentHasValue(in.Current, c.predicate, c.value) {
				// Restating a like reinforces the existing belief, never duplicates.
				actions = append(actions, buildAction(ActionReinforce, c, in, false))
				continue
			}
			actions = append(actions, buildAction(ActionCreate, c, in, false))
			continue
		}

		switch {
		case cur == nil:
			actions = append(actions, buildAction(ActionCreate, c, in, false))
		case entryValueString(*cur) == c.value:
			// Restating the current belief reinforces it (no new fact).
			actions = append(actions, buildAction(ActionReinforce, c, in, false))
		default:
			actions = append(actions, buildAction(ActionSupersede, c, in, c.correction))
		}
	}
	return actions
}

// buildAction assembles a fully-formed entry and its persistence mode.
func buildAction(mode ActionMode, c candidate, in Input, correction bool) Action {
	srcKind := SourceUserDeclared
	conf := declaredConfidence
	if correction {
		srcKind = SourceUserCorrected
		conf = correctedConfidence
	}
	ts := in.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	value, _ := RawValue(c.value)
	return Action{
		Mode: mode,
		Entry: Entry{
			ID:         newEntryID(),
			Kind:       c.kind,
			Subject:    entrySubjectUser,
			Predicate:  c.predicate,
			Value:      value,
			Status:     StatusCurrent,
			Confidence: conf,
			Sources: []Source{{
				Type:      SourceConversation,
				Kind:      srcKind,
				Ref:       in.SessionID + ":" + in.MessageID,
				Timestamp: ts,
			}},
			CreatedAt: ts,
			UpdatedAt: ts,
		},
		Rule: c.rule,
	}
}

// currentEntry returns the current entry for subject "user" and the predicate,
// or nil. Status-less entries passed in are treated as current.
func currentEntry(entries []Entry, predicate string) *Entry {
	for i := range entries {
		e := &entries[i]
		if e.Status != "" && e.Status != StatusCurrent {
			continue
		}
		if e.Subject == entrySubjectUser && e.Predicate == predicate {
			return e
		}
	}
	return nil
}

// currentHasValue reports whether any current entry for the predicate already
// carries the value (used for additive likes).
func currentHasValue(entries []Entry, predicate, value string) bool {
	for i := range entries {
		e := &entries[i]
		if e.Subject != entrySubjectUser || e.Predicate != predicate {
			continue
		}
		if e.Status != "" && e.Status != StatusCurrent {
			continue
		}
		if entryValueString(*e) == value {
			return true
		}
	}
	return false
}

// entryValueString reads a string entry value for comparison.
func entryValueString(e Entry) string {
	var s string
	if err := json.Unmarshal(e.Value, &s); err == nil {
		return s
	}
	return string(e.Value)
}

// trimClause cuts a captured value at the first " and " or " but " so a
// multi-clause message yields the clause that introduced the fact, never a
// value that bleeds into the following clause.
func trimClause(s string) string {
	low := strings.ToLower(s)
	for _, sep := range []string{" and ", " but "} {
		if i := strings.Index(low, sep); i >= 0 {
			return s[:i]
		}
	}
	return s
}

// stripTrailingNow removes a trailing " now" ("I live in Bangkok now").
func stripTrailingNow(s string) string {
	if loc := trailingNowRE.FindStringIndex(s); loc != nil {
		return s[:loc[0]]
	}
	return s
}

// cleanValue trims surrounding whitespace and quotes.
func cleanValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}

// rejectFirstWord reports whether the first word of a captured value is in the
// given reject list, so a rule can defer a declaration to a more specific rule.
func rejectFirstWord(v string, words []string) bool {
	fields := strings.Fields(strings.ToLower(v))
	if len(fields) == 0 {
		return false
	}
	for _, w := range words {
		if fields[0] == w {
			return true
		}
	}
	return false
}

// rejectLikesValue rejects like-captures whose first word is low-signal, so
// "I like that idea" and "I like it when you ..." never become preferences.
func rejectLikesValue(v string) bool {
	fields := strings.Fields(strings.ToLower(v))
	if len(fields) == 0 {
		return true
	}
	_, bad := likesStopwords[fields[0]]
	return bad
}
