package permissions

import (
	"strings"
	"testing"
)

func TestProposeCalendarAdd(t *testing.T) {
	p, rej, ok := ProposeStanding("Always let Ghost add calendar events")
	if !ok || len(p.Grants) == 0 {
		t.Fatalf("must propose: %+v %+v", p, rej)
	}
	if p.Grants[0].Capability != "calendar.create" || p.Grants[0].Scope != "owner" {
		t.Fatalf("narrow grant wrong: %+v", p.Grants)
	}
	if !strings.Contains(p.Summary, "Nothing else changes") {
		t.Fatal("summary must bound the grant")
	}
}

func TestBroadRejected(t *testing.T) {
	for _, text := range []string{
		"Always let Ghost access my entire Google account",
		"Always let Ghost do everything",
		"Let Ghost always access all my data",
	} {
		p, rej, ok := ProposeStanding(text)
		if !ok {
			t.Fatalf("%q must match standing intent", text)
		}
		if len(p.Grants) != 0 || rej.Reason == "" {
			t.Fatalf("%q must reject broad scope: %+v", text, p)
		}
	}
}

func TestMalformedRejected(t *testing.T) {
	p, rej, ok := ProposeStanding("Always let Ghost transcend spacetime")
	if !ok || len(p.Grants) != 0 || rej.Reason == "" {
		t.Fatalf("unknown scope must reject with guidance: %+v", p)
	}
	if len(rej.Options) == 0 {
		t.Fatal("rejection must offer valid options")
	}
}

func TestNeverIsDeny(t *testing.T) {
	p, _, ok := ProposeStanding("Never let Ghost control my lights")
	if !ok || !p.Deny || len(p.Grants) == 0 {
		t.Fatalf("deny proposal wrong: %+v", p)
	}
	if p.Grants[0].Capability != "hass.control" {
		t.Fatalf("deny must target exact capability: %+v", p.Grants)
	}
}

func TestNonStandingIgnored(t *testing.T) {
	for _, text := range []string{"what's the weather", "allow once", "yes"} {
		if _, _, ok := ProposeStanding(text); ok {
			t.Fatalf("%q is not a standing request", text)
		}
	}
}

func TestValidatedGrantsUnknownRejected(t *testing.T) {
	if out := validatedGrants([]StandingGrant{{"evil.cap", "x", "owner"}}); len(out) != 0 {
		t.Fatal("undeclared capability must fail closed")
	}
	if out := validatedGrants([]StandingGrant{{"calendar.create", "create", "context:work"}}); len(out) != 0 {
		t.Fatal("context scopes not expressible here")
	}
}

func TestNaturalGrantPhrases(t *testing.T) {
	cases := map[string]bool{
		"Always let Ghost add calendar events":      true,
		"You can always add calendar events for me": true,
		"I can always let you add reminders":        true,
		"Never let Ghost control my lights":         true,
		"what's the weather":                        false,
		"yes":                                       false,
	}
	for text, wantStanding := range cases {
		_, _, ok := ProposeStanding(text)
		if ok != wantStanding {
			t.Fatalf("%q: standing=%v want %v", text, ok, wantStanding)
		}
	}
	p, _, ok := ProposeStanding("You can always add calendar events for me")
	if !ok || len(p.Grants) != 1 || p.Grants[0].Capability != "calendar.create" {
		t.Fatalf("natural grant must propose narrow calendar.create: %+v", p)
	}
}

func TestBroadAccountRejectedDeterministically(t *testing.T) {
	for _, text := range []string{
		"You can do anything you want on my account",
		"You can access anything on my Google account",
		"do whatever you want with my data",
		"manage everything on my account",
	} {
		p, rej, ok := ProposeStanding(text)
		if !ok {
			t.Fatalf("%q must be a handled standing intent (never routed to the model as ordinary chat)", text)
		}
		if len(p.Grants) != 0 || rej.Reason == "" {
			t.Fatalf("%q must be rejected with no grant: proposal=%+v", text, p)
		}
	}
}
