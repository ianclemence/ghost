package personalcontext

import (
	"testing"
)

// "My favorite team is Chelsea" must land in personal context
// deterministically — it previously fell through every rule and only
// survived in the flat memory file.
func TestExtractFavoriteTeam(t *testing.T) {
	actions, err := Extract(testInput("My favorite team is Chelsea"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want exactly 1", len(actions))
	}
	e := actions[0].Entry
	if e.Kind != KindPreference {
		t.Errorf("kind = %q, want preference", e.Kind)
	}
	if e.Predicate != "preference/favorite_team" {
		t.Errorf("predicate = %q, want preference/favorite_team", e.Predicate)
	}
	if got := entryString(e); got != "Chelsea" {
		t.Errorf("value = %q, want Chelsea", got)
	}
}

// A changed allegiance supersedes: exactly one current team survives.
func TestExtractFavoriteTeamSupersedes(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	applyText(t, s, "My favorite team is Chelsea")
	applyText(t, s, "My favorite team is Arsenal")
	current := 0
	for _, e := range s.Current() {
		if e.Predicate == "preference/favorite_team" && e.Status == StatusCurrent {
			current++
			if got := entryString(e); got != "Arsenal" {
				t.Errorf("current team = %q, want Arsenal", got)
			}
		}
	}
	if current != 1 {
		t.Errorf("current favorite_team rows = %d, want 1", current)
	}
}

// Interrogatives must never be filed as names: "what is my girlfriend's
// name?" states no fact. Declarative forms keep working.
func TestExtractPartnerQuestionYieldsNothing(t *testing.T) {
	for _, q := range []string{
		"what is my girlfriend's name?",
		"Quick check — what is my girlfriend's name and which team do I support?",
		"who is my wife?",
	} {
		actions, err := Extract(testInput(q))
		if err != nil {
			t.Fatalf("Extract(%q): %v", q, err)
		}
		for _, a := range actions {
			if a.Entry.Predicate == "relationship/partner" {
				t.Errorf("Extract(%q) filed partner=%q", q, entryString(a.Entry))
			}
		}
	}
}

func TestExtractPartnerDeclarationStillWorks(t *testing.T) {
	actions, err := Extract(testInput("Jasmine is my girlfriend"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, a := range actions {
		if a.Entry.Predicate == "relationship/partner" && entryString(a.Entry) == "Jasmine" {
			found = true
		}
	}
	if !found {
		t.Fatalf("declaration not extracted: %+v", actions)
	}
}

func TestExtractStripsTrailingTemporalFiller(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"My favorite team is Arsenal now", "Arsenal"},
		{"My favorite team is Arsenal, for now", "Arsenal"},
		{"my favorite color is blue today", "blue"},
	} {
		actions, err := Extract(testInput(tc.text))
		if err != nil {
			t.Fatalf("Extract(%q): %v", tc.text, err)
		}
		if len(actions) != 1 {
			t.Fatalf("%q produced %d actions, want 1", tc.text, len(actions))
		}
		if got := actionValue(t, actions[0]); got != tc.want {
			t.Errorf("%q value = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestDirectiveEchoRejected(t *testing.T) {
	for _, v := range []string{"Remember this:", "remember this", "Note that:", "capture this", "save that:"} {
		if !DirectiveEcho(v) {
			t.Errorf("DirectiveEcho(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"Ian", "Chelsea", "mango sticky rice", "I would like to go to Bangkok some time"} {
		if DirectiveEcho(v) {
			t.Errorf("DirectiveEcho(%q) = true, want false", v)
		}
	}
}
