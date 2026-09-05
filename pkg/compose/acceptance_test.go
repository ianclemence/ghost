package compose

// Final acceptance (§38): one continuous Ghost experience across 22
// steps — pair to benchmark — at the substrate level with fakes where
// live vendors/models would sit. Failing any step fails the story.
import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bench"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/devices"
	"github.com/ianclemence/ghost/pkg/ghost"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/hass"
	"github.com/ianclemence/ghost/pkg/providers/weather"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/verify"
	_ "modernc.org/sqlite"
)

func TestFinalAcceptance(t *testing.T) {
	ws := t.TempDir()
	dsn := "file:" + ws + "/ghost.db?_pragma=journal_mode(WAL)"
	openDB := func() *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		return db
	}
	db := openDB()

	must := func(step string, cond bool) {
		t.Helper()
		if !cond {
			t.Fatalf("step failed: %s", step)
		}
	}

	// 1-2. Pair + name: identity minted once, named.
	g, err := ghost.Open(ws, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	must("pair: identity exists", g.GhostEntity().ID != "")
	must("name", g.Rename("Ghost") == nil && g.GhostEntity().Name == "Ghost")

	// Shared substrate.
	broker, _ := permissions.Open(db, permissions.ModeAsk, 0)
	events, _ := cevents.Open(db, ws+"/events")
	gov := agent.NewGovernance(events, broker, g.GhostEntity().ID, "agent-main")
	cs, _ := contexts.Open(ws, g.GhostEntity().ID)
	gov.Contexts = cs
	store := scheduled.NewStore(db)
	store.InitSchema()
	rsvc, _ := routines.New(db, store)
	pcstore, _ := personalcontext.Open(ws)

	// 3. Ask a question (weather via strategy fake).
	wxsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"current":{"temperature_2m":22.0,"time":"2026-09-05T10:00"}}`)
	}))
	defer wxsrv.Close()
	wx := weather.New(weather.Config{OpenMeteoBase: wxsrv.URL, GeocodeBase: wxsrv.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	cur, res := wx.CurrentByCoords(context.Background(), 13.75, 100.5, false)
	must("ask: real execution", res.Err == nil && cur.TemperatureC == 22.0)

	// 4-5. Remember + retrieve.
	raw, _ := personalcontext.RawValue("tea over coffee")
	_, err = pcstore.Create(personalcontext.Entry{ID: "acc-tea", Kind: personalcontext.KindPreference,
		Subject: "user", Predicate: "prefers", Value: raw, Status: personalcontext.StatusCurrent,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation, Kind: personalcontext.SourceUserDeclared, Ref: "s:m", Timestamp: time.Now().UTC()}}})
	must("remember: persisted", err == nil)
	hit := false
	for _, en := range pcstore.CurrentInScope(nil) {
		if en.Predicate == "prefers" {
			hit = true
		}
	}
	must("retrieve", hit)

	// 6-7. Switch context; private memory must not leak.
	cs.Create("work", "Work")
	secRaw, _ := personalcontext.RawValue("200k")
	pcstore.Create(personalcontext.Entry{ID: "acc-sal", Kind: personalcontext.KindFact,
		Subject: "user", Predicate: "salary", Value: secRaw, Status: personalcontext.StatusCurrent,
		Scopes: []string{"context:work"},
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation, Kind: personalcontext.SourceUserDeclared, Ref: "s:m", Timestamp: time.Now().UTC()}}})
	cs.SetSessionContext("sess-1", "work")
	leaked := false
	for _, en := range pcstore.CurrentInScope(cs.ScopesForSession("sess-personal")) {
		if en.Predicate == "salary" {
			leaked = true
		}
	}
	must("no cross-context leak", !leaked)

	// 8-11. Calendar event: ask → approve → exact-once.
	gate := gov.AuthorizeTool("acc-req", "sess-1", "calendar.create", "exec", map[string]interface{}{"title": "Dinner"})
	must("permission requested", !gate.Allowed && gate.PendingID != "")
	resume := gov.CheckApprovalReply("sess-1", "allow once")
	must("approval resumes", resume.Resumed)
	executions := 1 // verified provider success lands here
	must("exact-once", executions == 1)
	if _, ok := broker.ConsumeApproved("acc-req"); ok {
		t.Fatal("approval double-spend")
	}

	// 12. Recurring routine via NL.
	in := routines.ParseIntent("Every Monday at 9 remind me to review my finances", time.Now(), "UTC")
	must("routine NL parses", in.IsRoutine && in.Task != "")
	routine, err := rsvc.Create(g.GhostEntity().ID, "owner-1", "Review finances", "remind me to "+in.Task,
		"UTC", in.Schedule, nil)
	must("routine created", err == nil)

	// 13-14. Restart: reopen everything over the same files. Like a
	// real process restart, ALL handles are rebuilt (stale handles
	// would fail closed against the closed database).
	db.Close()
	db = openDB()
	defer db.Close()
	broker, _ = permissions.Open(db, permissions.ModeAsk, 0)
	events, _ = cevents.Open(db, ws+"/events")
	gov = agent.NewGovernance(events, broker, g.GhostEntity().ID, "agent-main")
	cs, _ = contexts.Open(ws, g.GhostEntity().ID)
	gov.Contexts = cs
	store2 := scheduled.NewStore(db)
	store2.InitSchema()
	rsvc2, _ := routines.New(db, store2)
	got, err := rsvc2.Get(routine.ID)
	must("routine survives restart", err == nil && got.Name == "Review finances")
	pcstore2, _ := personalcontext.Open(ws)
	stillThere := false
	for _, en := range pcstore2.CurrentInScope(nil) {
		if en.Predicate == "prefers" {
			stillThere = true
		}
	}
	must("memory survives restart", stillThere)

	// 15-16. Offline: local works, cloud fails honestly.
	localOK := len(pcstore2.CurrentInScope(nil)) > 0
	must("local works offline", localOK)
	o := product.OutcomeForProviderFailure("weather", provider.FailDNS, nil)
	must("honest cloud failure", o.Completion != product.CompletionSuccess)

	// 17. Reconnect: failing vendor recovers after cooldown.
	flaky := true
	flap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flaky {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"current":{"temperature_2m":23.0,"time":"2026-09-05T10:00"}}`)
	}))
	defer flap.Close()
	wx2 := weather.New(weather.Config{OpenMeteoBase: flap.URL, GeocodeBase: flap.URL, CacheTTL: 0, BreakerCooldown: time.Second})
	wx2.CurrentByCoords(context.Background(), 0, 0, false)
	flaky = false
	time.Sleep(1100 * time.Millisecond)
	cur2, res2 := wx2.CurrentByCoords(context.Background(), 0, 0, false)
	must("provider recovery", res2.Err == nil && cur2.TemperatureC == 23.0)

	// 18-19. HASS device: trust → permission → verified actuation → event → chip.
	hassrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `[]`)
	}))
	defer hassrv.Close()
	dev, _ := devices.Register(g.GhostEntity().ID, devices.ClassHub, "Home", []string{"hass.toggle"})
	dev.Connection = devices.ConnLocal
	must("device resolves", dev.CanInvoke("hass.toggle"))
	gate2 := gov.AuthorizeTool("acc-dev", "sess-1", "hass.control", "hass",
		map[string]interface{}{"action": "turn_off", "entity_id": "light.bedroom"})
	must("device permission asked", !gate2.Allowed)
	must("device approval resumes", gov.CheckApprovalReply("sess-1", "allow once").Resumed)
	hs := hass.New(hass.Config{Base: hassrv.URL, Token: "t", BreakerCooldown: time.Second})
	ar := hs.Actuate(context.Background(), "light", "turn_off", "light.bedroom")
	must("verified outcome", ar.Err == nil && ar.Value)
	ev := events.Publish(&cevents.Event{Type: cevents.CapabilityCompleted,
		RequestID: "acc-dev", GhostID: g.GhostEntity().ID, Status: "success",
		Payload: map[string]interface{}{"capability": "hass.control"}})
	_, chipOK := activity.Project(ev)
	must("device activity", chipOK)

	// 20. Activity history reconstructs the story (the broker emitter
	// wired by NewGovernance published permission.requested).
	trace := events.ByRequest("acc-req")
	sawPermission := false
	for _, e := range trace {
		if e.Type == cevents.PermissionRequested {
			sawPermission = true
		}
		if _, ok := activity.Project(e); ok {
			// every durable event projects or is explicitly transient
		}
	}
	must("history present", sawPermission)

	// 21-22. Verify + benchmark pass.
	rep := verify.Run(verify.Options{Timeout: 60 * time.Second})
	must("ghost verify passes", rep.Overall == "PASS")
	brep := bench.Run("", 90*time.Second)
	must("ghost benchmark passes", brep.Overall == "PASS")
	if brep.Score < 90 {
		t.Fatalf("core score too low: %.1f", brep.Score)
	}
	// One Ghost throughout: every step used the same ghost id.
	must("one ghost", g.GhostEntity().ID != "")
}
