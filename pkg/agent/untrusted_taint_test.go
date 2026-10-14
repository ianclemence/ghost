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
