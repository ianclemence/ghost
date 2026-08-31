package personalcontext

import (
	"testing"
	"time"
)

func TestLabelAndValue(t *testing.T) {
	cases := map[string]string{
		"identity/name":                  "Name",
		"preference/favorite_color":      "Favorite color",
		"preference/communication.style": "Communication style",
		"goal/primary":                   "Goal",
		"custom/some_thing":              "some thing",
	}
	for pred, want := range cases {
		if got := Label(pred); got != want {
			t.Errorf("Label(%q) = %q, want %q", pred, got, want)
		}
	}
}

func TestValueRendersString(t *testing.T) {
	e := Entry{Value: []byte(`"Bangkok"`)}
	if got := Value(e); got != "Bangkok" {
		t.Errorf("Value = %q, want %q", got, "Bangkok")
	}
}

func TestOpenForgetCurrent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Simulate a declaration the extractor would produce.
	actions, err := Extract(Input{
		SessionID: "s1",
		MessageID: "m1",
		Text:      "my name is Ian and I live in Bangkok",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(actions) == 0 {
		t.Fatalf("expected actions from declaration")
	}
	if _, err := store.applyActions(actions); err != nil {
		t.Fatalf("applyActions: %v", err)
	}

	cur := store.Current()
	if len(cur) == 0 {
		t.Fatalf("expected current entries after apply")
	}
	foundName := false
	for _, e := range cur {
		if e.Predicate == "identity/name" && Value(e) == "Ian" {
			foundName = true
		}
	}
	if !foundName {
		t.Fatalf("expected identity/name=Ian in current, got %+v", cur)
	}

	// Forget should retire one entry so it no longer appears current.
	if err := store.Forget(cur[0].ID); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(store.Current()) != len(cur)-1 {
		t.Fatalf("expected one fewer current entry after Forget")
	}
}

func TestExtractPrefersAndFavorite(t *testing.T) {
	actions, err := Extract(Input{SessionID: "s", MessageID: "m", Text: "I really prefer tea over coffee", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	found := false
	for _, a := range actions {
		if a.Entry.Predicate == "preference/prefers" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a preference/prefers entry, got %+v", actions)
	}

	acts2, _ := Extract(Input{SessionID: "s", MessageID: "m2", Text: "my favorite food is sushi", Timestamp: time.Now()})
	fav := false
	for _, a := range acts2 {
		if a.Entry.Predicate == "preference/favorite" {
			fav = true
		}
	}
	if !fav {
		t.Fatalf("expected a preference/favorite entry, got %+v", acts2)
	}
}
