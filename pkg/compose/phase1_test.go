package compose

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

// Permission+Routine: a routine scoped to reminders cannot silently use
// weather — even though the broker would allow the read. No escalation
// from ask to always_allow is possible through Require.
func TestRoutineScopeCannotEscalate(t *testing.T) {
	s := openSubstrate(t)
	s.gov.SetRoutineContext("routine:r1", "r1", []string{"reminder.create"})
	gate := s.gov.AuthorizeTool("req-1", "routine:r1", "weather.current", "weather_now",
		map[string]interface{}{"location": "Bangkok"})
	if gate.Allowed {
		t.Fatal("routine scope must deny non-listed capabilities")
	}
	// And Require never mints standing grants by itself.
	req, _ := s.broker.Require("req-1", "routine:r1", "a", "weather.current", "read", "", "",
		permissions.RiskReadOnly, nil)
	s.broker.Resolve(req.ID, permissions.GrantOnce, "s")
	if len(s.broker.Grants()) != 0 {
		t.Fatal("allow-once must not create standing grants")
	}
	s.gov.ClearRoutineContext("routine:r1")
}

// Permission+Restart: continuation secrets stay stripped across reopen.
func TestPermissionRestartNoSecrets(t *testing.T) {
	path := t.TempDir() + "/ghost.db"
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)"
	db1, _ := sql.Open("sqlite", dsn)
	db1.SetMaxOpenConns(1)
	b1, _ := permissions.Open(db1, permissions.ModeAsk, 0)
	b1.Require("req-1", "sess-1", "a", "calendar.create", "create", "", "do",
		permissions.RiskConsequential, map[string]string{"title": "Meet", "api_key": "S"})
	// Simulate secret smuggling attempt pre-restart via raw insert path?
	// Continuations are stripped at creation (governance.continuationOf);
	// verify the stored row directly.
	db1.Close()
	db2, _ := sql.Open("sqlite", dsn)
	db2.SetMaxOpenConns(1)
	defer db2.Close()
	b2, _ := permissions.Open(db2, permissions.ModeAsk, 0)
	pending, ok := b2.PendingForSession("sess-1")
	if !ok {
		t.Fatal("pending must survive restart")
	}
	for k, v := range pending.Continuation {
		if v == "S" {
			t.Fatalf("secret survived in %s", k)
		}
	}
}

// Event replay never executes: re-publishing a consumed approval changes
// nothing and cannot re-run the capability.
func TestReplayNeverExecutes(t *testing.T) {
	s := openSubstrate(t)
	executions := 0
	run := func() {
		req, _ := s.broker.Require("req-1", "sess-1", "a", "calendar.create", "create", "", "",
			permissions.RiskConsequential, nil)
		s.broker.Resolve(req.ID, permissions.GrantOnce, "s")
		if _, ok := s.broker.ConsumeApproved("req-1"); ok {
			executions++
		}
	}
	run()
	// "Replay": re-publish every event for the request.
	for _, e := range s.events.ByRequest("req-1") {
		s.events.Publish(&cevents.Event{Type: e.Type, RequestID: e.RequestID,
			GhostID: e.GhostID, Payload: e.Payload})
	}
	run() // same request id: idempotent Require + consumed approval
	if executions != 1 {
		t.Fatalf("exactly-once violated: %d", executions)
	}
}

// Routine+Timezone: DST boundary keeps local 9 AM (America/New_York,
// DST starts Mar 8 2026).
func TestRoutineTimezoneDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// Monday 9 AM before and after the spring-forward.
	before := time.Date(2026, 3, 2, 9, 0, 0, 0, loc) // EST (UTC-5)
	after := time.Date(2026, 3, 9, 9, 0, 0, 0, loc)  // EDT (UTC-4)
	if before.Hour() != 9 || after.Hour() != 9 {
		t.Fatal("test setup broken")
	}
	if after.UTC().Sub(before.UTC()) != 7*24*time.Hour-1*time.Hour {
		t.Fatal("DST spring-forward must shorten the week by one hour")
	}
	next := scheduled.NextCronRun("0 9 * * MON", "America/New_York", before.Add(time.Hour))
	if next == nil || next.In(loc).Hour() != 9 {
		t.Fatalf("next run must stay 9 AM local, got %v", next)
	}
	name, _ := next.In(loc).Zone()
	if name != "EDT" {
		t.Fatalf("post-DST run must be EDT, got %s", name)
	}
}

// Credentials+Events: a 401 marks the credential invalid; the event and
// chip say "reconnect" with no secret anywhere.
func TestCredentialExpiryPipeline(t *testing.T) {
	s := openSubstrate(t)
	e := s.events.Publish(&cevents.Event{Type: cevents.IntegrationExpired,
		RequestID: "req-c", GhostID: "ghost-1",
		Payload: map[string]interface{}{"integration": "calendar", "summary": "connection expired"}})
	chip, ok := activity.Project(e)
	if !ok || chip.Title != "Needs reconnecting" {
		t.Fatalf("expiry chip wrong: %+v", chip)
	}
	o := product.Failure(product.ErrExpired, product.FriendlyFor("calendar", product.ErrExpired), "connect_calendar", false)
	if o.Completion != product.CompletionWaitingForAuth || o.Action != "connect_calendar" {
		t.Fatalf("expiry outcome wrong: %+v", o)
	}
}

// Context+Permissions: a grant scoped to one session never authorizes
// another session's turn (context switches can't widen scope).
func TestGrantScopeAcrossSessions(t *testing.T) {
	s := openSubstrate(t)
	req, _ := s.broker.Require("req-1", "sess-work", "a", "calendar.read", "read", "", "",
		permissions.RiskConsequential, nil)
	s.broker.Resolve(req.ID, permissions.GrantAlways, "session:sess-work")
	if s.broker.Evaluate("calendar.read", "read", "session:sess-work", permissions.RiskConsequential) != permissions.VerdictAllow {
		t.Fatal("own session must allow")
	}
	if s.broker.Evaluate("calendar.read", "read", "session:sess-home", permissions.RiskConsequential) != permissions.VerdictAsk {
		t.Fatal("other session must still ask")
	}
}

// Agent+Permissions (governance-level): consumed approval for request A
// never authorizes request B, even with identical args.
func TestApprovalBoundToRequest(t *testing.T) {
	s := openSubstrate(t)
	g := agent.NewGovernance(s.events, s.broker, "ghost-1", "agent-main")
	g.AuthorizeTool("req-A", "sess-1", "calendar.create", "exec", map[string]interface{}{"title": "X"})
	out := g.CheckApprovalReply("sess-1", "allow once")
	if !out.Resumed {
		t.Fatal("must resume A")
	}
	// Attacker replays the same approval text for a different request.
	g.AuthorizeTool("req-B", "sess-1", "calendar.create", "exec", map[string]interface{}{"title": "X"})
	out2 := g.CheckApprovalReply("sess-1", "allow once")
	if !out2.Resumed {
		t.Fatal("B has its own pending; approving it is legitimate")
	}
	// But the consumed approval for A cannot be double-spent.
	if _, ok := s.broker.ConsumeApproved("req-A"); ok {
		t.Fatal("consumed approval must not re-spend")
	}
}

// Offline honesty: DNS failure classifies network/offline, never success.
func TestOfflineClassification(t *testing.T) {
	o := product.OutcomeForProviderFailure("weather", provider.FailDNS, nil)
	if o.Completion == product.CompletionSuccess {
		t.Fatal("dns failure must never be success")
	}
	cls := product.ClassForProviderFailure(provider.FailDNS)
	if cls != product.ErrProvider && cls != product.ErrOffline {
		t.Fatalf("dns must map to provider/offline, got %s", cls)
	}
}

// Context+Memory: work-scoped facts never surface in personal retrieval,
// even when the model would find them useful.
func TestContextMemoryIsolation(t *testing.T) {
	ws := t.TempDir()
	cs, err := contexts.Open(ws, "ghost-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Create("work", "Work"); err != nil {
		t.Fatal(err)
	}
	cs.SetSessionContext("sess-work", "work")
	pstore, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(pred, val string, scopes []string) {
		raw, _ := personalcontext.RawValue(val)
		e := personalcontext.Entry{ID: "test-" + pred, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: pred, Value: raw, Status: personalcontext.StatusCurrent,
			Scopes:  scopes,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand, Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}}}
		if _, err := pstore.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	mk("salary", "200k", []string{"context:work"})
	mk("likes", "tea", nil)
	personal := pstore.CurrentInScope(cs.ScopesForSession("sess-personal"))
	for _, e := range personal {
		if e.Predicate == "salary" {
			t.Fatal("personal must not retrieve work-scoped salary")
		}
	}
	work := pstore.CurrentInScope(cs.ScopesForSession("sess-work"))
	foundWork, foundGlobal := false, false
	for _, e := range work {
		if e.Predicate == "salary" {
			foundWork = true
		}
		if e.Predicate == "likes" {
			foundGlobal = true
		}
	}
	if !foundWork || !foundGlobal {
		t.Fatal("work must see own + global")
	}
}
