package health

import (
	"testing"
)

func TestAggregateReady(t *testing.T) {
	m := New()
	for _, n := range AllSubsystems {
		m.Update(Subsystem{Name: n, State: StateReady, Status: "Ready.", Reason: "ok"})
	}
	r := m.Report()
	if r.Overall != StateReady {
		t.Fatalf("expected ready, got %s", r.Overall)
	}
}

func TestHealthVsConfiguration(t *testing.T) {
	m := New()
	for _, n := range AllSubsystems {
		m.Update(Subsystem{Name: n, State: StateReady, Status: "Ready.", Reason: "ok"})
	}
	// Calendar authorized-but-provider-down vs not-authorized must differ.
	m.SetIntegration("calendar", Subsystem{State: StateNeedsAuthorization, Status: "Calendar isn't connected.", Reason: "calendar_not_authorized"})
	r := m.Report()
	got := r.Subsystems["integration:calendar"]
	if got.State != StateNeedsAuthorization || got.Reason != "calendar_not_authorized" {
		t.Fatalf("must preserve auth-not-configured distinctly: %+v", got)
	}
	m.SetIntegration("calendar", Subsystem{State: StateTemporarilyUnavailable, Status: "Calendar provider down.", Reason: "calendar_provider_5xx"})
	got = r.Subsystems["integration:calendar"]
	_ = got
	r = m.Report()
	if r.Subsystems["integration:calendar"].Reason != "calendar_provider_5xx" {
		t.Fatal("provider-down must be distinct from not-authorized")
	}
	m.SetIntegration("calendar", Subsystem{State: StateRevoked, Status: "Access revoked.", Reason: "calendar_revoked"})
	r = m.Report()
	if r.Subsystems["integration:calendar"].State != StateRevoked {
		t.Fatal("revoked must be distinct")
	}
}

func TestCriticalForcesAction(t *testing.T) {
	m := New()
	m.Update(Subsystem{Name: Core, State: StateReady, Status: "ok", Reason: "ok"})
	m.Update(Subsystem{Name: Security, State: StateDegraded, Status: "bad", Reason: "x", Severity: SeverityCritical})
	if r := m.Report(); r.Overall != StateActionRequired {
		t.Fatalf("critical must force action_required, got %s", r.Overall)
	}
}

func TestRollup(t *testing.T) {
	m := New()
	m.SetIntegration("flight", Subsystem{State: StateNotConfigured, Status: "Flight tracking isn't connected yet.", Reason: "flight_not_configured"})
	if r := m.Report(); r.Subsystems[Integrations].State != StateDegraded {
		t.Fatal("integrations rollup must degrade on attention")
	}
}
