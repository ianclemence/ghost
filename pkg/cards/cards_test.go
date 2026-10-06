package cards

import (
	"testing"
	"time"
)

func TestValidateKinds(t *testing.T) {
	for _, k := range []Kind{KindSuggestion, KindGoalUpdate, KindCart, KindBrowserView} {
		c, err := New(k, "Title", "Body")
		if err != nil {
			t.Fatalf("kind %s must validate: %v", k, err)
		}
		if c.ID == "" || c.CreatedAt.IsZero() {
			t.Fatal("card must carry id + timestamp")
		}
	}
	if _, err := New(KindCheckoutSheet, "Pay", "x"); err == nil {
		t.Fatal("checkout_sheet must refuse without a payment partner")
	}
	if _, err := New("bogus", "T", "B"); err == nil {
		t.Fatal("unknown kinds must refuse")
	}
	if _, err := New(KindSuggestion, "", "B"); err == nil {
		t.Fatal("title is required")
	}
}

func TestTextFallback(t *testing.T) {
	c, err := New(KindSuggestion, "Dinner idea", "Try the new Thai place.")
	if err != nil {
		t.Fatal(err)
	}
	c.Actions = []Action{{ID: "a", Label: "Yes"}}
	fb := c.TextFallback()
	if fb == "" {
		t.Fatal("fallback must not be empty")
	}
	for _, want := range []string{"Dinner idea", "Thai place", "[Yes]"} {
		found := false
		for i := 0; i+len(want) <= len(fb); i++ {
			if fb[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fallback must contain %q: %q", want, fb)
		}
	}
}

func TestStoreCapsAndExpiry(t *testing.T) {
	s := &Store{}
	for i := 0; i < storeCap+5; i++ {
		c, err := New(KindSuggestion, "T", "B")
		if err != nil {
			t.Fatal(err)
		}
		s.Add("mobile", c)
	}
	if got := len(s.List("mobile")); got != storeCap {
		t.Fatalf("store must cap at %d, got %d", storeCap, got)
	}
	old, err := New(KindSuggestion, "Old", "B")
	if err != nil {
		t.Fatal(err)
	}
	old.ExpiresAt = time.Now().Add(-time.Hour)
	s2 := &Store{}
	s2.Add("mobile", old)
	if got := len(s2.List("mobile")); got != 0 {
		t.Fatal("expired cards must be pruned")
	}
}

func TestResolveByDataPutsAwayCompanionCards(t *testing.T) {
	s := &Store{}
	c1, err := New(KindSuggestion, "S", "B1")
	if err != nil {
		t.Fatal(err)
	}
	c1.Data = map[string]interface{}{"idea_id": "idea-1"}
	c1.Actions = []Action{{ID: "approve", Label: "Yes"}, {ID: "deny", Label: "No thanks"}}
	c2, err := New(KindSuggestion, "S", "B2")
	if err != nil {
		t.Fatal(err)
	}
	c2.Data = map[string]interface{}{"idea_id": "idea-1"}
	c2.Actions = []Action{{ID: "approve", Label: "Yes"}}
	c3, err := New(KindSuggestion, "S", "B3")
	if err != nil {
		t.Fatal(err)
	}
	c3.Data = map[string]interface{}{"idea_id": "idea-2"}
	s.Add("mobile", c1)
	s.Add("web", c2)
	s.Add("mobile", c3)

	done := s.ResolveByData("idea_id", "idea-1", "deny")
	if len(done) != 2 {
		t.Fatalf("must resolve both companions, got %d", len(done))
	}
	byID := map[string]Card{}
	for _, c := range done {
		byID[c.ID] = c
	}
	if byID[c1.ID].Resolved == nil || byID[c1.ID].Resolved.ActionID != "deny" {
		t.Fatalf("offered deny must win: %+v", byID[c1.ID].Resolved)
	}
	if byID[c2.ID].Resolved == nil || byID[c2.ID].Resolved.ActionID != "dismiss" {
		t.Fatalf("unoffered deny must fall back to dismiss: %+v", byID[c2.ID].Resolved)
	}
	// Stored state carries the receipt; serving filters it (see openCards).
	stored, _ := s.Find("mobile", c1.ID)
	if stored.Resolved == nil {
		t.Fatal("resolution must persist in the store")
	}
	if open, _ := s.Find("mobile", c3.ID); open.Resolved != nil {
		t.Fatal("unrelated card must stay open")
	}
	// Repeat decisions are idempotent: nothing left to resolve.
	if again := s.ResolveByData("idea_id", "idea-1", "deny"); len(again) != 0 {
		t.Fatalf("re-deciding must resolve nothing, got %d", len(again))
	}
}
