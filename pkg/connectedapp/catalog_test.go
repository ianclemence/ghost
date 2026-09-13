package connectedapp

import "testing"

func TestFirstPartyCatalog(t *testing.T) {
	list := FirstParty()
	if len(list) < 6 {
		t.Fatalf("expected >=6 first-party connectors, got %d", len(list))
	}
	seen := map[string]bool{}
	for _, c := range list {
		if c.ID == "" || c.Provider == "" || c.DisplayName == "" {
			t.Fatalf("incomplete connector: %+v", c)
		}
		if len(c.Capabilities) == 0 {
			t.Fatalf("connector %s must declare capabilities", c.ID)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate connector %s", c.ID)
		}
		seen[c.ID] = true
	}
	for _, id := range []string{"gmail", "google-calendar", "outlook", "home-assistant", "spotify", "github"} {
		if !seen[id] {
			t.Fatalf("missing first-party connector %s", id)
		}
	}
}

func TestChannelVsAppSeparation(t *testing.T) {
	for _, ch := range []string{"telegram", "slack", "discord", "whatsapp", "sms"} {
		if !IsChannelCredential(ch) {
			t.Fatalf("%s must be classified as channel transport", ch)
		}
		if _, ok := ByID(ch); ok {
			t.Fatalf("%s must never be a connected-app manifest", ch)
		}
	}
	for _, m := range []string{"openai", "anthropic"} {
		if !IsModelCredential(m) {
			t.Fatalf("%s must be classified as model credential", m)
		}
	}
	for _, o := range []string{"gmail", "outlook", "spotify", "google-calendar"} {
		if !IsOAuthOnly(o) {
			t.Fatalf("%s must require browser sign-in", o)
		}
	}
	if IsOAuthOnly("github") || IsOAuthOnly("openweather") {
		t.Fatal("paste-key apps must not require OAuth")
	}
}
