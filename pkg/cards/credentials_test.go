package cards

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestACredentialRequestNamesOnlyASiteAndWhy(t *testing.T) {
	c, err := NewCredentialRequest("https://www.ads.google.com/home", "Google Ads Manager", "Pause the campaigns that are over budget.")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != KindCredentials || c.Title != "Sign in to Google Ads Manager" || CredentialHostOf(c) != "ads.google.com" {
		t.Fatalf("card: %+v", c)
	}
	if time.Until(c.ExpiresAt) > time.Hour {
		t.Fatalf("a sign-in request is for now, not for a day: expires %v", c.ExpiresAt)
	}
	b, _ := json.Marshal(c)
	for _, forbidden := range []string{"password", "secret", "token"} {
		if strings.Contains(strings.ToLower(string(b)), forbidden) {
			t.Fatalf("the card record should hold no field for %q: %s", forbidden, b)
		}
	}
}

func TestACredentialRequestRefusesWhatIsNotASite(t *testing.T) {
	for _, bad := range []string{"", "localhost", "127.0.0.1", "10.0.0.1:8080", "not a site", "javascript:alert(1)", "a", "https://"} {
		if _, err := NewCredentialRequest(bad, "X", ""); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if got := CredentialHost("HTTPS://WWW.Example.COM:8443/login?x=1"); got != "example.com" {
		t.Errorf("host = %q", got)
	}
}

func TestResolveAsKeepsTheOwnersWordsOutOfTheRecord(t *testing.T) {
	s := &Store{items: map[string][]Card{}}
	c, _ := NewCredentialRequest("ads.google.com", "Google Ads", "")
	s.Add("mobile", c)
	if _, ok := s.ResolveAs("mobile", c.ID, "not-offered", "x"); ok {
		t.Fatal("an action the card did not offer must not resolve it")
	}
	if _, ok := s.ResolveAs("mobile", c.ID, "submitted", "  "); ok {
		t.Fatal("a blank outcome must not resolve it")
	}
	got, ok := s.ResolveAs("mobile", c.ID, "submitted", "Signed in to ads.google.com")
	if !ok || got.Resolved == nil || got.Resolved.Label != "Signed in to ads.google.com" {
		t.Fatalf("not resolved: %+v %v", got, ok)
	}
	if _, ok := s.ResolveAs("mobile", c.ID, "submitted", "again"); ok {
		t.Fatal("a card resolves once")
	}
	if ch, found, ok := s.FindAnywhere(c.ID); !ok || ch != "mobile" || found.ID != c.ID {
		t.Fatal("a card should be findable by id")
	}
}
