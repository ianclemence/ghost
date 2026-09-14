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
	for i := 0; i < 25; i++ {
		c, err := New(KindSuggestion, "T", "B")
		if err != nil {
			t.Fatal(err)
		}
		s.Add("mobile", c)
	}
	if got := len(s.List("mobile")); got != 20 {
		t.Fatalf("store must cap at 20, got %d", got)
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
