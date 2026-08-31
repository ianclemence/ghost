package personalcontext

import (
	"strings"
	"testing"
)

// The extractor is pure rule matching over explicit statements: none of these
// tests require an LLM, Ollama, or any model service (acceptance F). It never
// reads or writes conversation evidence.

func testInput(text string) Input {
	return Input{
		SessionID: "telegram:42",
		MessageID: "msg-77",
		Text:      text,
		Timestamp: fixedTime,
	}
}

func actionValue(t *testing.T, a Action) string {
	t.Helper()
	var s string
	if err := a.Entry.ValueInto(&s); err != nil {
		t.Fatalf("entry %s value %s is not a string: %v", a.Entry.ID, a.Entry.Value, err)
	}
	return s
}

func entryString(e Entry) string {
	var s string
	_ = e.ValueInto(&s)
	return s
}

// A. Explicit declaration produces exactly one well-formed candidate with
// conversation provenance and high confidence.
func TestExtractDeclaration(t *testing.T) {
	actions, err := Extract(Input{
		SessionID: "s1",
		MessageID: "m1",
		Text:      "my favorite color is blue",
		Timestamp: fixedTime,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want exactly 1", len(actions))
	}
	a := actions[0]
	if a.Mode != ActionCreate {
		t.Errorf("mode = %q, want create", a.Mode)
	}
	e := a.Entry
	if e.Kind != KindPreference {
		t.Errorf("kind = %q, want preference", e.Kind)
	}
	if e.Subject != entrySubjectUser {
		t.Errorf("subject = %q, want user", e.Subject)
	}
	if e.Predicate != "preference/favorite_color" {
		t.Errorf("predicate = %q, want preference/favorite_color", e.Predicate)
	}
	if got := actionValue(t, a); got != "blue" {
		t.Errorf("value = %q, want blue", got)
	}
	if e.Status != StatusCurrent {
		t.Errorf("status = %q, want current", e.Status)
	}
	if e.Confidence < 0.9 {
		t.Errorf("confidence = %v, want >= 0.9", e.Confidence)
	}
	if len(e.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(e.Sources))
	}
	src := e.Sources[0]
	if src.Type != SourceConversation {
		t.Errorf("source type = %q, want conversation", src.Type)
	}
	if src.Kind != SourceUserDeclared {
		t.Errorf("source kind = %q, want user_declared", src.Kind)
	}
	if src.Ref != "s1:m1" {
		t.Errorf("source ref = %q, want s1:m1", src.Ref)
	}
	if !src.Timestamp.Equal(fixedTime) {
		t.Errorf("source timestamp = %s, want %s", src.Timestamp, fixedTime)
	}
}

// Every supported declaration form maps to the documented kind, predicate,
// and value, with user_declared provenance.
func TestExtractDeclarationTable(t *testing.T) {
	cases := []struct {
		text      string
		rule      string
		kind      Kind
		predicate string
		value     string
	}{
		{"my favorite color is blue", "favorite_color", KindPreference, "preference/favorite_color", "blue"},
		{"my name is Ian", "name", KindIdentity, "identity/name", "Ian"},
		{"I live in Bangkok", "location", KindFact, "fact/location", "Bangkok"},
		{"I prefer concise answers", "communication_style", KindPreference, "preference/communication.style", "concise"},
		{"I like dark chocolate", "likes", KindPreference, "preference/likes", "dark chocolate"},
		{"my goal is to launch X", "goal", KindGoal, "goal/primary", "launch X"},
		{"I want to build X", "want_build", KindGoal, "goal/primary", "build X"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			actions, err := Extract(testInput(tc.text))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %d, want 1: %+v", len(actions), actions)
			}
			a := actions[0]
			if a.Rule != tc.rule {
				t.Errorf("rule = %q, want %q", a.Rule, tc.rule)
			}
			if a.Mode != ActionCreate {
				t.Errorf("mode = %q, want create", a.Mode)
			}
			if a.Entry.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", a.Entry.Kind, tc.kind)
			}
			if a.Entry.Predicate != tc.predicate {
				t.Errorf("predicate = %q, want %q", a.Entry.Predicate, tc.predicate)
			}
			if got := actionValue(t, a); got != tc.value {
				t.Errorf("value = %q, want %q", got, tc.value)
			}
			if a.Entry.Sources[0].Kind != SourceUserDeclared {
				t.Errorf("source kind = %q, want user_declared", a.Entry.Sources[0].Kind)
			}
			if a.Entry.Confidence < 0.9 {
				t.Errorf("confidence = %v, want >= 0.9", a.Entry.Confidence)
			}
		})
	}
}

// B. An explicit correction supersedes the current entry; the old value is
// retired and the new one becomes current with user_corrected provenance and
// confidence 1.0.
func TestExtractCorrectionSupersedes(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	if _, err := s.Create(mkEntry("pc-blue", "user", "preference/favorite_color", "blue")); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	actions, err := Apply(s, Input{
		SessionID: "s1",
		MessageID: "m2",
		Text:      "actually, my favorite color is green",
		Timestamp: fixedTime,
		Current:   s.Current(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	a := actions[0]
	if a.Mode != ActionSupersede {
		t.Errorf("mode = %q, want supersede", a.Mode)
	}
	if got := actionValue(t, a); got != "green" {
		t.Errorf("value = %q, want green", got)
	}
	if a.Entry.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", a.Entry.Confidence)
	}
	if len(a.Entry.Sources) != 1 || a.Entry.Sources[0].Kind != SourceUserCorrected {
		t.Errorf("sources = %+v, want user_corrected", a.Entry.Sources)
	}

	cur := s.Current()
	if len(cur) != 1 || entryString(cur[0]) != "green" {
		t.Fatalf("Current = %+v, want only green", cur)
	}
	old, _ := s.Get("pc-blue")
	if old.Status != StatusSuperseded {
		t.Errorf("pc-blue status = %q, want superseded", old.Status)
	}
	if old.SupersededBy == nil || *old.SupersededBy != cur[0].ID {
		t.Errorf("pc-blue superseded_by = %v, want %s", old.SupersededBy, cur[0].ID)
	}
}

// Every supported correction form supersedes the seeded value.
func TestExtractCorrectionTable(t *testing.T) {
	cases := []struct {
		text      string
		predicate string
		value     string
	}{
		{"actually, my favorite color is green", "preference/favorite_color", "green"},
		{"actually I live in Bangkok now", "fact/location", "Bangkok"},
		{"that's wrong, my name is Ian", "identity/name", "Ian"},
		{"correction: I prefer concise answers", "preference/communication.style", "concise"},
		{"no, my favorite color is green", "preference/favorite_color", "green"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			ws := t.TempDir()
			s := mustOpen(t, ws)
			if _, err := s.Create(mkEntry("pc-seed", "user", tc.predicate, "old")); err != nil {
				t.Fatalf("seed: %v", err)
			}

			actions, err := Apply(s, Input{
				SessionID: "s1",
				MessageID: "m2",
				Text:      tc.text,
				Timestamp: fixedTime,
				Current:   s.Current(),
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %d, want 1", len(actions))
			}
			a := actions[0]
			if a.Mode != ActionSupersede {
				t.Errorf("mode = %q, want supersede", a.Mode)
			}
			if got := actionValue(t, a); got != tc.value {
				t.Errorf("value = %q, want %q", got, tc.value)
			}
			if a.Entry.Sources[0].Kind != SourceUserCorrected {
				t.Errorf("source kind = %q, want user_corrected", a.Entry.Sources[0].Kind)
			}
			if a.Entry.Confidence != 1.0 {
				t.Errorf("confidence = %v, want 1.0", a.Entry.Confidence)
			}
			if cur := s.Current(); len(cur) != 1 || entryString(cur[0]) != tc.value {
				t.Fatalf("Current = %+v, want only %q", cur, tc.value)
			}
		})
	}
}

// C. The explicit memory command language is a declaration, not a correction.
func TestExtractRememberTable(t *testing.T) {
	cases := []struct {
		text      string
		predicate string
		value     string
	}{
		{"remember that my favorite color is blue", "preference/favorite_color", "blue"},
		{"remember that I live in Bangkok", "fact/location", "Bangkok"},
		{"remember: I prefer concise answers", "preference/communication.style", "concise"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			actions, err := Extract(testInput(tc.text))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %d, want 1", len(actions))
			}
			a := actions[0]
			if a.Mode != ActionCreate {
				t.Errorf("mode = %q, want create", a.Mode)
			}
			if a.Entry.Predicate != tc.predicate {
				t.Errorf("predicate = %q, want %q", a.Entry.Predicate, tc.predicate)
			}
			if got := actionValue(t, a); got != tc.value {
				t.Errorf("value = %q, want %q", got, tc.value)
			}
			if a.Entry.Sources[0].Kind != SourceUserDeclared {
				t.Errorf("source kind = %q, want user_declared", a.Entry.Sources[0].Kind)
			}
			if a.Entry.Confidence < 0.9 {
				t.Errorf("confidence = %v, want >= 0.9", a.Entry.Confidence)
			}
		})
	}
}

// D. Ordinary conversation is never silently inferred into facts.
func TestExtractOrdinaryConversation(t *testing.T) {
	for _, text := range []string{
		"I had coffee this morning.",
		"What time is it?",
		"Please help me debug this function.",
		"thanks!",
		"That's great news.",
	} {
		actions, err := Extract(testInput(text))
		if err != nil {
			t.Fatalf("Extract(%q): %v", text, err)
		}
		if len(actions) != 0 {
			t.Errorf("%q produced %d actions, want 0", text, len(actions))
		}
	}
}

// E. A deictic correction with no resolvable previous predicate yields no
// candidate rather than guessing.
func TestExtractAmbiguousCorrection(t *testing.T) {
	actions, err := Extract(testInput("actually, it's green"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %d, want 0 (no resolvable predicate)", len(actions))
	}
}

// A deictic correction IS resolved when the immediately preceding user turn
// yields exactly one unambiguous declaration.
func TestExtractPronounResolvedFromPreviousTurn(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	if _, err := s.Create(mkEntry("pc-blue", "user", "preference/favorite_color", "blue")); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	actions, err := Apply(s, Input{
		SessionID:    "s1",
		MessageID:    "m2",
		Text:         "actually, it's green",
		Timestamp:    fixedTime,
		PreviousText: "my favorite color is blue",
		Current:      s.Current(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	a := actions[0]
	if a.Mode != ActionSupersede {
		t.Errorf("mode = %q, want supersede", a.Mode)
	}
	if a.Entry.Predicate != "preference/favorite_color" {
		t.Errorf("predicate = %q, want preference/favorite_color", a.Entry.Predicate)
	}
	if got := actionValue(t, a); got != "green" {
		t.Errorf("value = %q, want green", got)
	}
	if a.Entry.Sources[0].Kind != SourceUserCorrected {
		t.Errorf("source kind = %q, want user_corrected", a.Entry.Sources[0].Kind)
	}
	if cur := s.Current(); len(cur) != 1 || entryString(cur[0]) != "green" {
		t.Fatalf("Current = %+v, want only green", cur)
	}
}

// A previous turn with multiple declarations is not unambiguous.
func TestExtractPronounAmbiguousPrevious(t *testing.T) {
	actions, err := Extract(Input{
		SessionID:    "s1",
		MessageID:    "m2",
		Text:         "actually, it's green",
		Timestamp:    fixedTime,
		PreviousText: "my name is Ian and I live in Bangkok",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %d, want 0 (ambiguous previous turn)", len(actions))
	}
}

// Restating the current belief, as a declaration or as a correction, changes
// nothing.
func TestExtractRestatementSkipped(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	if _, err := s.Create(mkEntry("pc-blue", "user", "preference/favorite_color", "blue")); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	for _, text := range []string{
		"my favorite color is blue",
		"actually, my favorite color is blue",
	} {
		actions, err := Extract(Input{
			SessionID: "s1",
			MessageID: "m2",
			Text:      text,
			Timestamp: fixedTime,
			Current:   s.Current(),
		})
		if err != nil {
			t.Fatalf("Extract(%q): %v", text, err)
		}
		if len(actions) != 1 || actions[0].Mode != ActionReinforce {
			t.Errorf("%q produced %d actions, want one reinforce (restatement)", text, len(actions))
		}
	}

	if cur := s.Current(); len(cur) != 1 || entryString(cur[0]) != "blue" {
		t.Fatalf("Current = %+v, want unchanged blue", cur)
	}
}

// Contradicting evidence supersedes even without an explicit correction
// marker: supersession is primary.
func TestExtractDeclarationDiffersSupersedes(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	if _, err := s.Create(mkEntry("pc-blue", "user", "preference/favorite_color", "blue")); err != nil {
		t.Fatalf("create blue: %v", err)
	}

	actions, err := Apply(s, Input{
		SessionID: "s1",
		MessageID: "m2",
		Text:      "my favorite color is green",
		Timestamp: fixedTime,
		Current:   s.Current(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(actions) != 1 || actions[0].Mode != ActionSupersede {
		t.Fatalf("actions = %+v, want one supersede", actions)
	}
	if actions[0].Entry.Sources[0].Kind != SourceUserDeclared {
		t.Errorf("source kind = %q, want user_declared (no correction marker)", actions[0].Entry.Sources[0].Kind)
	}
	if cur := s.Current(); len(cur) != 1 || entryString(cur[0]) != "green" {
		t.Fatalf("Current = %+v, want only green", cur)
	}
}

// Likes are additive: different likes coexist as current entries and are never
// superseded; restating an existing like is skipped.
func TestExtractLikesAdditive(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	apply := func(text string) []Action {
		t.Helper()
		actions, err := Apply(s, Input{
			SessionID: "s1",
			MessageID: "m2",
			Text:      text,
			Timestamp: fixedTime,
			Current:   s.Current(),
		})
		if err != nil {
			t.Fatalf("Apply(%q): %v", text, err)
		}
		return actions
	}

	apply("I like dark chocolate")

	a2 := apply("I like milk chocolate")
	if len(a2) != 1 || a2[0].Mode != ActionCreate {
		t.Fatalf("second like actions = %+v, want one create", a2)
	}

	if a3 := apply("I like dark chocolate"); len(a3) != 1 || a3[0].Mode != ActionReinforce {
		t.Fatalf("restated like produced %d actions, want one reinforce", len(a3))
	}

	likes := s.ByPredicate("preference/likes")
	if len(likes) != 2 {
		t.Fatalf("likes entries = %d, want 2", len(likes))
	}
	for _, e := range likes {
		if e.Status != StatusCurrent {
			t.Errorf("like %q status = %q, want current (additive, never superseded)", entryString(e), e.Status)
		}
	}
}

// Low-signal like captures ("I like that", "I like how ...") are rejected.
func TestExtractLikesRejectsLowSignal(t *testing.T) {
	for _, text := range []string{
		"I like that",
		"I like it when you use short answers",
		"I like how you did that",
		"I like your shirt",
	} {
		actions, err := Extract(testInput(text))
		if err != nil {
			t.Fatalf("Extract(%q): %v", text, err)
		}
		if len(actions) != 0 {
			t.Errorf("%q produced %d actions, want 0 (low-signal like)", text, len(actions))
		}
	}
}

// One message can declare several facts; each is a separate action, values
// keep the user's casing, and the first " and " cuts the value cleanly.
func TestExtractMultiFact(t *testing.T) {
	actions, err := Extract(testInput("My name is Ian and I live in Bangkok"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(actions))
	}
	byPred := map[string]string{}
	for _, a := range actions {
		byPred[a.Entry.Predicate] = actionValue(t, a)
	}
	if byPred["identity/name"] != "Ian" {
		t.Errorf("identity/name = %q, want Ian (case preserved)", byPred["identity/name"])
	}
	if byPred["fact/location"] != "Bangkok" {
		t.Errorf("fact/location = %q, want Bangkok (case preserved)", byPred["fact/location"])
	}
}

// Values are cleaned: trailing punctuation and surrounding quotes are dropped,
// and clause punctuation bounds the capture.
func TestExtractValueCleaning(t *testing.T) {
	cases := []struct {
		text  string
		value string
	}{
		{"my favorite color is blue.", "blue"},
		{`my favorite color is "blue"`, "blue"},
		{"I live in New York, NY", "New York"},
		{"my goal is to launch X!", "launch X"},
		{"My Favorite Color Is Teal", "Teal"},
	}
	for _, tc := range cases {
		actions, err := Extract(testInput(tc.text))
		if err != nil {
			t.Fatalf("Extract(%q): %v", tc.text, err)
		}
		if len(actions) != 1 {
			t.Fatalf("%q produced %d actions, want 1", tc.text, len(actions))
		}
		if got := actionValue(t, actions[0]); got != tc.value {
			t.Errorf("%q value = %q, want %q", tc.text, got, tc.value)
		}
	}
}

// Empty input is a valid no-op.
func TestExtractEmptyText(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t"} {
		actions, err := Extract(testInput(text))
		if err != nil {
			t.Fatalf("Extract(%q): %v", text, err)
		}
		if len(actions) != 0 {
			t.Errorf("empty input produced %d actions, want 0", len(actions))
		}
	}
}

// Extracted ids are ec_-prefixed ULIDs (26 Crockford base32 characters).
func TestExtractedEntryIDFormat(t *testing.T) {
	actions, err := Extract(testInput("my favorite color is blue"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	id := actions[0].Entry.ID
	if !strings.HasPrefix(id, "ec_") {
		t.Errorf("id %q does not start with ec_", id)
	}
	if len(id) != len("ec_")+26 {
		t.Errorf("id %q length = %d, want %d", id, len(id), len("ec_")+26)
	}
	for _, r := range id[len("ec_"):] {
		if !strings.ContainsRune(crockford, r) {
			t.Errorf("id %q contains non-Crockford char %q", id, r)
		}
	}
}

// G. Extract + persist + reopen the store preserves the current context.
func TestApplyPersistsAndReloads(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	actions, err := Apply(s, Input{
		SessionID: "s1",
		MessageID: "m1",
		Text:      "my name is Ian and I live in Bangkok",
		Timestamp: fixedTime,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(actions))
	}

	reopened := mustOpen(t, ws)
	cur := reopened.Current()
	if len(cur) != 2 {
		t.Fatalf("Current after reload = %d entries, want 2: %+v", len(cur), cur)
	}
	byPred := map[string]string{}
	for _, e := range cur {
		byPred[e.Predicate] = entryString(e)
	}
	if byPred["identity/name"] != "Ian" {
		t.Errorf("identity/name = %q, want Ian", byPred["identity/name"])
	}
	if byPred["fact/location"] != "Bangkok" {
		t.Errorf("fact/location = %q, want Bangkok", byPred["fact/location"])
	}
}

// Scope 6: a correction with no matching current entry becomes a new
// declaration instead of failing.
func TestExtractCorrectionWithNoCurrentEntry(t *testing.T) {
	actions, err := Extract(Input{
		SessionID: "s1",
		MessageID: "m1",
		Text:      "actually, my favorite color is green",
		Timestamp: fixedTime,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	a := actions[0]
	if a.Mode != ActionCreate {
		t.Errorf("mode = %q, want create (no current entry to correct)", a.Mode)
	}
	if a.Entry.Sources[0].Kind != SourceUserDeclared {
		t.Errorf("source kind = %q, want user_declared fallback", a.Entry.Sources[0].Kind)
	}
	if a.Entry.Confidence != declaredConfidence {
		t.Errorf("confidence = %v, want %v", a.Entry.Confidence, declaredConfidence)
	}
}
