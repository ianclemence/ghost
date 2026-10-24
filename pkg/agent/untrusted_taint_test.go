package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
)

func TestOutwardActAsksAfterUntrustedRead(t *testing.T) {
	g := openTestGovernance(t)
	if err := g.Broker.GrantStanding("message.send", "message", permissions.ScopeFor("sess", "", ""), false); err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{}

	clean := g.AuthorizeStandalone("req-clean", "sess", "message.send", "message", args, permissions.RiskConsequential)
	if !clean.Allowed {
		t.Fatalf("a standing grant must still allow a clean turn: %+v", clean)
	}

	g.MarkUntrustedRead("req-tainted")
	tainted := g.AuthorizeStandalone("req-tainted", "sess", "message.send", "message", args, permissions.RiskConsequential)
	if tainted.Allowed || tainted.PendingID == "" {
		t.Fatalf("outward act after reading outside content must ask: %+v", tainted)
	}
}

func TestUntrustedReadDoesNotBlockReadOnly(t *testing.T) {
	g := openTestGovernance(t)
	g.MarkUntrustedRead("req-1")
	res := g.AuthorizeStandalone("req-1", "sess", "weather.current", "weather_now", map[string]interface{}{}, permissions.RiskReadOnly)
	if !res.Allowed {
		t.Fatalf("reading must stay free after an untrusted read: %+v", res)
	}
}

func TestUntrustedReadEndsWithTurn(t *testing.T) {
	g := openTestGovernance(t)
	g.MarkUntrustedRead("req-1")
	g.TurnEnded("req-1", "sess", "", nil)
	if g.readUntrusted("req-1") {
		t.Fatal("taint outlived its turn")
	}
}

// A hostile page can tell the model to send mail or run a command. The task
// grant the owner gave the browser must not carry over to those.
func TestPageReadStopsOtherOutwardActsButNotTheBrowserItself(t *testing.T) {
	g := openTestGovernance(t)
	if err := g.Broker.GrantStanding("message.send", "message", permissions.ScopeFor("sess", "", ""), false); err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{}

	g.MarkPageRead("req-page")
	sent := g.AuthorizeStandalone("req-page", "sess", "message.send", "message", args, permissions.RiskConsequential)
	if sent.Allowed || sent.PendingID == "" {
		t.Fatalf("an outward act after reading a page must ask: %+v", sent)
	}
	if v := g.evaluateTurn("req-page", browserCapability, "browser_click", permissions.ScopeFor("sess", "", ""), permissions.RiskReadOnly); v != permissions.VerdictAllow {
		t.Fatalf("reading in the browser stays free: %v", v)
	}
	if g.readPage("req-other") {
		t.Fatal("the mark belongs to one request")
	}
	g.TurnEnded("req-page", "sess", "", nil)
	if g.readPage("req-page") {
		t.Fatal("the mark outlived its turn")
	}
}
