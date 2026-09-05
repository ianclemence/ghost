// Package health is the single source of truth for appliance health.
//
// It covers Core, Local AI, Memory, Storage, Security, Network, Remote
// Access, Automations, Integrations, Backup, and Updates. Each subsystem
// reports state + human-readable status + machine-readable reason +
// last-checked + remediation + severity.
//
// The Web Console consumes this model; it must not rebuild health logic
// independently. Health is distinct from configuration: "calendar healthy
// but not authorized" differs from "authorized but provider down" differs
// from "credential revoked".
package health

import (
	"sync"
	"time"
)

// State is the machine-readable subsystem state.
type State string

const (
	StateReady                  State = "ready"
	StateDegraded               State = "degraded"
	StateNotConfigured          State = "not_configured"
	StateNeedsAuthorization     State = "needs_authorization"
	StateNeedsPermission        State = "needs_permission"
	StateExpired                State = "expired"
	StateRevoked                State = "revoked"
	StateTemporarilyUnavailable State = "temporarily_unavailable"
	StateOffline                State = "offline"
	StateActionRequired         State = "action_required"
	StateUnknown                State = "unknown"
)

// Severity ranks how much attention a subsystem needs.
type Severity string

const (
	SeverityOK       Severity = "ok"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Subsystem is one health entry.
type Subsystem struct {
	Name        string    `json:"name"`
	State       State     `json:"state"`
	Status      string    `json:"status"` // human-readable, product language
	Reason      string    `json:"reason"` // machine-readable code, e.g. "calendar_not_authorized"
	LastChecked time.Time `json:"last_checked"`
	Remediation string    `json:"remediation,omitempty"` // product-level next step
	Action      string    `json:"action,omitempty"`      // e.g. "connect_calendar"
	Severity    Severity  `json:"severity"`
}

// Known subsystem names.
const (
	Core         = "core"
	LocalAI      = "local_ai"
	Memory       = "memory"
	Storage      = "storage"
	Security     = "security"
	Network      = "network"
	RemoteAccess = "remote_access"
	Automations  = "automations"
	Integrations = "integrations"
	Backup       = "backup"
	Updates      = "updates"
)

// AllSubsystems lists every subsystem the aggregate covers.
var AllSubsystems = []string{Core, LocalAI, Memory, Storage, Security, Network, RemoteAccess, Automations, Integrations, Backup, Updates}

// Report is the whole-appliance view. Overall is derived, never set
// independently: READY only if nothing needs attention; DEGRADED if any
// warning; ACTION_REQUIRED if any critical/actionable item.
type Report struct {
	Overall    State                `json:"overall"`
	Summary    string               `json:"summary"`
	CheckedAt  time.Time            `json:"checked_at"`
	Subsystems map[string]Subsystem `json:"subsystems"`
}

// Model holds the latest health, updated by subsystem checkers.
type Model struct {
	mu   sync.RWMutex
	subs map[string]Subsystem
}

// New creates a model with all subsystems unknown.
func New() *Model {
	m := &Model{subs: map[string]Subsystem{}}
	for _, n := range AllSubsystems {
		m.subs[n] = Subsystem{Name: n, State: StateUnknown, Status: "Not checked yet.", Reason: "not_checked", Severity: SeverityInfo}
	}
	return m
}

// Update records a subsystem result.
func (m *Model) Update(s Subsystem) {
	if s.LastChecked.IsZero() {
		s.LastChecked = time.Now()
	}
	if s.Severity == "" {
		s.Severity = severityFor(s.State)
	}
	m.mu.Lock()
	m.subs[s.Name] = s
	m.mu.Unlock()
}

// Report derives the aggregate. Integrations is a rollup name: individual
// integration states (calendar, flight, ...) are stored under
// "integration:<id>" keys and summarized into Integrations.
func (m *Model) Report() Report {
	m.mu.RLock()
	defer m.mu.RUnlock()
	subs := make(map[string]Subsystem, len(m.subs))
	for k, v := range m.subs {
		subs[k] = v
	}
	overall := StateReady
	summary := "Ghost is ready."
	for _, s := range subs {
		// Per-integration detail keys don't drive the aggregate directly;
		// the Integrations rollup does.
		if len(s.Name) > 12 && s.Name[:12] == "integration:" {
			continue
		}
		switch s.State {
		case StateReady:
		case StateNotConfigured, StateNeedsAuthorization, StateNeedsPermission,
			StateExpired, StateRevoked:
			// Unconfigured integrations are normal (progressive
			// configuration), not defects: they lower the aggregate to
			// degraded only via the integrations rollup, handled below.
			if s.Name == Integrations {
				overall = StateDegraded
				summary = "Ghost is ready, with some integrations needing attention."
			}
		case StateDegraded, StateTemporarilyUnavailable, StateOffline:
			if overall == StateReady {
				overall = StateDegraded
				summary = "Ghost is running with reduced capability."
			}
		case StateActionRequired:
			overall = StateActionRequired
			summary = "Ghost needs your attention."
		default:
			if overall == StateReady && s.State != StateUnknown {
				overall = StateDegraded
				summary = "Ghost is running with reduced capability."
			}
		}
	}
	// Critical severity anywhere forces action-required.
	for _, s := range subs {
		if s.Severity == SeverityCritical {
			overall = StateActionRequired
			summary = "Ghost needs your attention."
			break
		}
	}
	return Report{Overall: overall, Summary: summary, CheckedAt: time.Now(), Subsystems: subs}
}

// SetIntegration records a single integration's health under
// "integration:<id>" and refreshes the Integrations rollup.
func (m *Model) SetIntegration(id string, s Subsystem) {
	s.Name = "integration:" + id
	m.Update(s)
	m.mu.Lock()
	rollup := Subsystem{Name: Integrations, State: StateReady, Status: "Integrations ready.", Reason: "all_ready", Severity: SeverityOK, LastChecked: time.Now()}
	for k, v := range m.subs {
		if len(k) <= 12 || k[:12] != "integration:" {
			continue
		}
		switch v.State {
		case StateReady:
		case StateNotConfigured, StateNeedsAuthorization, StateNeedsPermission, StateExpired, StateRevoked:
			rollup.State = StateDegraded
			rollup.Status = "Some integrations need attention."
			rollup.Reason = "integration_attention"
			rollup.Severity = SeverityInfo
			rollup.Remediation = "Open Integrations to connect or renew."
			rollup.Action = "open_integrations"
		default:
			rollup.State = StateDegraded
			rollup.Status = "Some integrations are unavailable."
			rollup.Reason = "integration_unavailable"
			rollup.Severity = SeverityWarning
		}
	}
	m.subs[Integrations] = rollup
	m.mu.Unlock()
}

func severityFor(s State) Severity {
	switch s {
	case StateReady:
		return SeverityOK
	case StateNotConfigured, StateNeedsAuthorization, StateNeedsPermission, StateExpired, StateRevoked:
		return SeverityInfo
	case StateDegraded, StateTemporarilyUnavailable, StateOffline, StateUnknown:
		return SeverityWarning
	case StateActionRequired:
		return SeverityCritical
	default:
		return SeverityWarning
	}
}
