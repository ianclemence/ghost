package permissions

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestBroker(t *testing.T, mode Mode) *Broker {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := Open(db, mode, 0)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRiskTable(t *testing.T) {
	if RiskOf("weather.current") != RiskReadOnly {
		t.Fatal("weather must be read-only")
	}
	if RiskOf("calendar.read") != RiskConsequential {
		t.Fatal("calendar must be consequential")
	}
	if RiskOf("reminder.create") != RiskLow {
		t.Fatal("reminder must be low-risk")
	}
	if RiskOf("nope.unknown") != RiskConsequential {
		t.Fatal("unknown must fail closed")
	}
}

func TestEvaluateReadOnlyAllows(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	if b.Evaluate("weather.current", "read", "owner", RiskReadOnly) != VerdictAllow {
		t.Fatal("read-only must allow even in ask mode")
	}
}

func TestEvaluateConsequentialAsks(t *testing.T) {
	for _, m := range []Mode{ModeAsk, ModeAuto, ModeCustom} {
		b := openTestBroker(t, m)
		if b.Evaluate("calendar.create", "create", "owner", RiskConsequential) != VerdictAsk {
			t.Fatalf("consequential must ask in %s", m)
		}
	}
	b := openTestBroker(t, ModeFull)
	if b.Evaluate("calendar.create", "create", "owner", RiskConsequential) != VerdictAllow {
		t.Fatal("full mode allows consequential")
	}
	if b.Evaluate("exec", "shell", "owner", RiskHighImpact) != VerdictAsk {
		t.Fatal("high-impact never auto-allows, even in full")
	}
}

func TestAllowOnceThenAskAgain(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	req, err := b.Require("req-1", "sess-1", "agent-main", "telegram.send", "send", "contact:maria", "Send message", RiskConsequential, map[string]string{"text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != StatusPending {
		t.Fatal("must be pending")
	}
	resolved, err := b.Resolve(req.ID, GrantOnce, "contact:maria")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusApproved {
		t.Fatal("must be approved")
	}
	// Allow-once must NOT persist: next identical request asks again.
	if b.Evaluate("telegram.send", "send", "contact:maria", RiskConsequential) != VerdictAsk {
		t.Fatal("allow-once must not persist")
	}
}

func TestAlwaysAllowScoped(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	req, _ := b.Require("req-1", "sess-1", "agent-main", "calendar.read", "read", "owner", "", RiskConsequential, nil)
	if _, err := b.Resolve(req.ID, GrantAlways, "owner"); err != nil {
		t.Fatal(err)
	}
	if b.Evaluate("calendar.read", "read", "owner", RiskConsequential) != VerdictAllow {
		t.Fatal("scoped grant must allow")
	}
	// Scope is exact: different scope still asks (no silent broadening).
	if b.Evaluate("calendar.read", "read", "other", RiskConsequential) != VerdictAsk {
		t.Fatal("grant must not broaden to other scopes")
	}
	// Different action still asks.
	if b.Evaluate("calendar.read", "delete", "owner", RiskConsequential) != VerdictAsk {
		t.Fatal("grant must not broaden to other actions")
	}
}

func TestDenyBlocks(t *testing.T) {
	b := openTestBroker(t, ModeFull)
	req, _ := b.Require("req-1", "sess-1", "agent-main", "hass.control", "toggle", "home", "", RiskConsequential, nil)
	if _, err := b.Resolve(req.ID, GrantDeny, "home"); err != nil {
		t.Fatal(err)
	}
	if b.Evaluate("hass.control", "toggle", "home", RiskConsequential) != VerdictDeny {
		t.Fatal("deny must block even in full mode")
	}
}

func TestExpiryUnusable(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	b, _ := Open(db, ModeAsk, time.Minute)
	b.nowFunc = time.Now
	req, _ := b.Require("req-1", "sess-1", "a", "c", "act", "", "", RiskConsequential, nil)
	// Travel past expiry.
	b.nowFunc = func() time.Time { return req.ExpiresAt.Add(time.Second) }
	if _, err := b.Resolve(req.ID, GrantOnce, "s"); err == nil {
		t.Fatal("expired approval must not resolve")
	}
	r, _ := b.byID(req.ID)
	if r.Status != StatusExpired {
		t.Fatalf("must be expired, got %s", r.Status)
	}
}

func TestRequireIdempotent(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	a, err := b.Require("req-1", "sess-1", "a", "c", "act", "", "", RiskConsequential, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.Require("req-1", "sess-1", "a", "c", "act", "", "", RiskConsequential, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != c.ID {
		t.Fatal("same turn must reuse the same request (no duplicate cards)")
	}
}

func TestPendingSurvivesReopen(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	b1, _ := Open(db, ModeAsk, 0)
	req, _ := b1.Require("req-9", "sess-1", "agent-main", "c", "act", "t", "r", RiskConsequential, map[string]string{"k": "v"})
	b2, _ := Open(db, ModeAsk, 0)
	got, ok := b2.PendingForRequest("req-9")
	if !ok || got.ID != req.ID || got.Continuation["k"] != "v" {
		t.Fatal("pending must survive broker reopen (restart)")
	}
}

func TestRevoke(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	req, _ := b.Require("req-1", "sess-1", "a", "calendar.read", "read", "", "", RiskConsequential, nil)
	b.Resolve(req.ID, GrantAlways, "owner")
	if len(b.Grants()) != 1 {
		t.Fatal("must list grants")
	}
	if err := b.Revoke("calendar.read", "read", "owner"); err != nil {
		t.Fatal(err)
	}
	if b.Evaluate("calendar.read", "read", "owner", RiskConsequential) != VerdictAsk {
		t.Fatal("revoked grant must ask again")
	}
}

func TestEmitterFires(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	var got []string
	b.SetEmitter(func(t string, r *Request) { got = append(got, t) })
	req, _ := b.Require("r1", "sess-1", "a", "c", "act", "", "", RiskConsequential, nil)
	b.Resolve(req.ID, GrantDeny, "s")
	if len(got) != 2 || got[0] != "permission.requested" || got[1] != "permission.denied" {
		t.Fatalf("bad lifecycle events: %v", got)
	}
}

func TestPendingForSession(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	if _, ok := b.PendingForSession("sess-9"); ok {
		t.Fatal("nothing pending yet")
	}
	req, _ := b.Require("req-1", "sess-9", "a", "c", "act", "", "", RiskConsequential, nil)
	got, ok := b.PendingForSession("sess-9")
	if !ok || got.ID != req.ID {
		t.Fatal("must find session pending")
	}
	if _, ok := b.PendingForSession("other"); ok {
		t.Fatal("other sessions must not see it")
	}
}

func TestConsumeApprovedOnce(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	req, _ := b.Require("req-1", "sess-1", "a", "c", "act", "", "", RiskConsequential, nil)
	if _, ok := b.ConsumeApproved("req-1"); ok {
		t.Fatal("unapproved must not consume")
	}
	b.Resolve(req.ID, GrantOnce, "s")
	got, ok := b.ConsumeApproved("req-1")
	if !ok || got.ID != req.ID {
		t.Fatal("must consume exactly once")
	}
	if _, ok := b.ConsumeApproved("req-1"); ok {
		t.Fatal("second consume must fail (exactly-once)")
	}
}

func TestApprovalCardShape(t *testing.T) {
	b := openTestBroker(t, ModeAsk)
	req, _ := b.Require("req-1", "sess-1", "a", "calendar.create", "create", "owner", "Add dinner with Sarah?",
		RiskConsequential, map[string]string{"title": "Dinner", "api_key": "S"})
	card, ok := req.Card()
	if !ok {
		t.Fatal("pending must project a card")
	}
	if card.Title != "Add calendar event?" {
		t.Fatalf("card title wrong: %q", card.Title)
	}
	if len(card.Actions) != 3 {
		t.Fatal("card must offer allow-once/always/deny")
	}
	raw := strings.ToLower(card.Title + card.Description)
	for _, banned := range []string{"api_key", "exec", "schema", "sk-", "/var/", "provider", "reasoning"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("card leaked %q", banned)
		}
	}
	// Resolved requests project no card.
	b.Resolve(req.ID, GrantDeny, "s")
	updated, _ := b.byID(req.ID)
	if _, ok := updated.Card(); ok {
		t.Fatal("non-pending must not project actionable cards")
	}
}
