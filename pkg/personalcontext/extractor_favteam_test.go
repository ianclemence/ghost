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
