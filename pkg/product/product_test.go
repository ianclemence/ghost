package product

import (
	"strings"
	"testing"
)

func TestCompletionTerminal(t *testing.T) {
	if !CompletionSuccess.Terminal() || !CompletionFailed.Terminal() {
		t.Fatal("success/failed must be terminal")
	}
	if CompletionWaitingForUser.Terminal() || CompletionWaitingForConfig.Terminal() {
		t.Fatal("waiting states must not be terminal")
	}
}

func TestFailureMapping(t *testing.T) {
	o := Failure(ErrConfigRequired, "x", "connect_calendar", false)
	if o.Completion != CompletionWaitingForConfig {
		t.Fatalf("got %s", o.Completion)
	}
	o = Failure(ErrAuthRequired, "x", "", false)
	if o.Completion != CompletionWaitingForAuth {
		t.Fatalf("got %s", o.Completion)
	}
	o = Failure(ErrRateLimited, "x", "", true)
	if o.Completion != CompletionTemporarilyUnavailable || !o.Retryable {
		t.Fatalf("rate limit mapping wrong: %+v", o)
	}
}

func TestFriendlyNoImplJargon(t *testing.T) {
	banned := []string{"API_KEY", "OAuth token", "cron", "circuit breaker", "provider circuit", "gcalcli", "Ollama", "SQLite", "systemd", "vector index"}
	for cap := range map[string]bool{"calendar": true, "flight": true, "weather": true, "reminder": true, "other": true} {
		for _, class := range []ErrorClass{ErrConfigRequired, ErrAuthRequired, ErrProvider, ErrRateLimited, ErrOffline, ErrClarification} {
			msg := FriendlyFor(cap, class)
			lower := strings.ToLower(msg)
			for _, b := range banned {
				if strings.Contains(lower, strings.ToLower(b)) {
					t.Fatalf("cap=%s class=%s leaked %q in %q", cap, class, b, msg)
				}
			}
		}
	}
	// Spot checks for the mandated product language.
	if !strings.Contains(FriendlyFor("flight", ErrConfigRequired), "Flight tracking isn't connected") {
		t.Fatal("flight message wrong")
	}
	if !strings.Contains(FriendlyFor("calendar", ErrAuthRequired), "calendar connection needs to be renewed") {
		t.Fatal("calendar message wrong")
	}
	if FriendlyFor("reminder", ErrClarification) != "What time should I remind you?" {
		t.Fatal("reminder clarification wrong")
	}
}

func TestVisibility(t *testing.T) {
	if !VisUserMessage.UserVisible() || !VisUserError.UserVisible() {
		t.Fatal("user categories must be visible")
	}
	for _, v := range []Visibility{VisInternalTrace, VisToolActivity, VisSystemEvent, VisModelContext} {
		if v.UserVisible() {
			t.Fatalf("%s must not be user visible", v)
		}
	}
	e := Event{Visibility: VisInternalTrace, Category: "provider.http.request", Summary: "GET ..."}
	if e.HumanActivity() != "" {
		t.Fatal("internal trace must not render in activity")
	}
	e2 := Event{Visibility: VisUserMessage, Category: "calendar.checked", Summary: "Checked your calendar"}
	if e2.HumanActivity() == "" {
		t.Fatal("user message must render")
	}
}
