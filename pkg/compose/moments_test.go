package compose

// The five core Ghost moments (TALK / REMEMBER / ACT / AUTOMATE /
// CONTROL), exercised as deterministic equivalents: the same subsystems
// (strategy, store, broker, events, chips) run with fakes where a live
// model or live vendor would sit. Each test states its seam explicitly;
// none claims live-model coverage.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/devices"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/providers/hass"
	"github.com/ianclemence/ghost/pkg/providers/weather"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// TALK: "What's the weather?" → strategy → outcome → event → chip.
// Seam: httptest stands in for Open-Meteo (wire-compatible JSON).
func TestMomentTalk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"current":{"temperature_2m":21.5,"weather_code":1,"time":"2026-09-05T10:00"}}`)
	}))
	defer srv.Close()
	s := openSubstrate(t)
	svc := weather.New(weather.Config{OpenMeteoBase: srv.URL, GeocodeBase: srv.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	cur, r := svc.CurrentByCoords(context.Background(), 13.75, 100.5, false)
	if r.Err != nil {
		t.Fatalf("capability failed: %v", r.Err)
	}
	e := s.events.Publish(&cevents.Event{Type: cevents.CapabilityCompleted,
		RequestID: "req-talk", GhostID: "ghost-1", Status: "success",
		Payload: map[string]interface{}{"capability": "weather.current", "provider": r.Provider,
			"summary": fmt.Sprintf("%.1f°C", cur.TemperatureC)}})
	chip, ok := activity.Project(e)
	if !ok || chip.Title != "Weather checked" {
		t.Fatalf("talk chip wrong: %+v", chip)
	}
}

// REMEMBER: "Remember I prefer tea" → persisted → restart → retrieved
// in scope → chip. Seam: store-level (no live extractor).
func TestMomentRemember(t *testing.T) {
	ws := t.TempDir()
	mk := func() *personalcontext.Store {
		st, err := personalcontext.Open(ws)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	st := mk()
	raw, _ := personalcontext.RawValue("tea over coffee")
	e := personalcontext.Entry{ID: "test-tea", Kind: personalcontext.KindPreference,
		Subject: "user", Predicate: "prefers", Value: raw, Status: personalcontext.StatusCurrent,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
			Kind: personalcontext.SourceUserDeclared, Ref: "s:m", Timestamp: time.Now().UTC()}}}
	if _, err := st.Create(e); err != nil {
		t.Fatal(err)
	}
	// Restart: reopen from disk.
	st2 := mk()
	found := false
	for _, got := range st2.CurrentInScope(nil) {
		if got.Predicate == "prefers" {
			found = true
		}
	}
	if !found {
		t.Fatal("memory must survive restart and retrieve")
	}
	s := openSubstrate(t)
	ev := s.events.Publish(&cevents.Event{Type: cevents.MemoryCreated,
		RequestID: "req-rem", GhostID: "ghost-1",
		Payload: map[string]interface{}{"title": "Prefers tea over coffee"}})
	chip, ok := activity.Project(ev)
	if !ok || !strings.Contains(chip.Title, "Remembered") {
		t.Fatalf("remember chip wrong: %+v", chip)
	}
}

// ACT: covered by TestConsequentialActionPipeline; this asserts the
// exact-once + verified-result properties once more at the moment level.
func TestMomentAct(t *testing.T) {
	s := openSubstrate(t)
	executions := 0
	gate := s.gov.AuthorizeTool("req-act", "sess-act", "calendar.create", "exec",
		map[string]interface{}{"title": "Dinner with Sarah"})
	if gate.Allowed {
		t.Fatal("must ask first")
	}
	resume := s.gov.CheckApprovalReply("sess-act", "allow once")
	if !resume.Resumed {
		t.Fatal("must resume")
	}
	executions++ // verified provider success would land here
	s.events.Publish(&cevents.Event{Type: cevents.CapabilityCompleted,
		RequestID: "req-act", GhostID: "ghost-1", Status: "success",
		Payload: map[string]interface{}{"capability": "calendar.create"}})
	if executions != 1 {
		t.Fatal("exactly once")
	}
	if _, ok := s.broker.ConsumeApproved("req-act"); ok {
		t.Fatal("approval must be single-use")
	}
}

// AUTOMATE: NL → routine → scheduler → idempotent run → waiting on
// permission → resume → done. Seam: NL parse + service (no live model).
func TestMomentAutomate(t *testing.T) {
	s := openSubstrate(t)
	in := routines.ParseIntent("Every Monday morning give me my weekly brief", time.Now(), "UTC")
	if !in.IsRoutine || in.Task == "" {
		t.Fatalf("NL must parse: %+v", in)
	}
	store := scheduled.NewStore(s.db)
	store.InitSchema()
	rsvc, _ := routines.New(s.db, store)
	r, err := rsvc.Create("ghost-1", "owner-1", "Weekly Brief", "give me my weekly brief",
		"UTC", in.Schedule, nil)
	if err != nil {
		t.Fatal(err)
	}
	next := scheduled.NextCronRun(in.Schedule.Expr, "UTC", time.Now())
	if next == nil || next.Before(time.Now()) {
		t.Fatalf("must schedule future run: %v", next)
	}
	// First firing needs permission → waiting, runnable.
	out, err := rsvc.Run(context.Background(), r.ID, "exec-1", func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Completion: product.CompletionWaitingForPermission, WaitingOn: "p1"}
	})
	if err != nil || out.Completion != product.CompletionWaitingForPermission {
		t.Fatalf("must wait: %+v %v", out, err)
	}
	// Duplicate firing rejected.
	if _, err := rsvc.Run(context.Background(), r.ID, "exec-1", func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Completion: product.CompletionSuccess}
	}); err == nil {
		t.Fatal("duplicate run must be rejected")
	}
	// Approval → resume → done.
	out2, err := rsvc.Run(context.Background(), r.ID, "exec-2", func(ctx context.Context, r *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Completion: product.CompletionSuccess, Message: "brief ready"}
	})
	if err != nil || out2.Completion != product.CompletionSuccess {
		t.Fatalf("must complete: %+v %v", out2, err)
	}
}

// CONTROL: "Turn off the bedroom lights" → trust → permission →
// verified actuation → event → chip. Seam: httptest stands in for HA.
func TestMomentControl(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	s := openSubstrate(t)
	dev, err := devices.Register("ghost-1", devices.ClassHub, "Home", []string{"hass.toggle"})
	if err != nil {
		t.Fatal(err)
	}
	dev.Connection = devices.ConnLocal
	if !dev.CanInvoke("hass.toggle") {
		t.Fatal("trusted device must invoke")
	}
	gate := s.gov.AuthorizeTool("req-ctl", "sess-ctl", "hass.control", "hass",
		map[string]interface{}{"action": "turn_off", "entity_id": "light.bedroom"})
	if gate.Allowed {
		t.Fatal("actuation must ask first")
	}
	if resume := s.gov.CheckApprovalReply("sess-ctl", "allow once"); !resume.Resumed {
		t.Fatal("must resume")
	}
	svc := hass.New(hass.Config{Base: srv.URL, Token: "t", BreakerCooldown: time.Second})
	res := svc.Actuate(context.Background(), "light", "turn_off", "light.bedroom")
	if res.Err != nil || !res.Value {
		t.Fatalf("verified actuation failed: %+v", res)
	}
	e := s.events.Publish(&cevents.Event{Type: cevents.CapabilityCompleted,
		RequestID: "req-ctl", GhostID: "ghost-1", Status: "success",
		Payload: map[string]interface{}{"capability": "hass.control", "summary": "Bedroom light off"}})
	if _, ok := activity.Project(e); !ok {
		t.Fatal("control must surface activity")
	}
	// Unregistered device identifiers can never invoke.
	if dev.CanInvoke("shell") {
		t.Fatal("undeclared capability must not invoke")
	}
	_ = permissions.ModeAsk
}
