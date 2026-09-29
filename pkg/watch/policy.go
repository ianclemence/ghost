package watch

import (
	"fmt"
	"time"
)

// Policy is the deterministic gate in front of every watch creation. It
// answers one question — may Ghost start watching this? — with a fixed
// order of checks and a machine-readable reason. The model is never asked.
type Policy struct {
	// Enabled is PROACTIVE_PREFERENCES.Enabled. It gates AUTOMATIC
	// creation only: an owner who asked for a watch by name gets one, the
	// same way an explicitly requested reminder runs under a disabled
	// proactive policy.
	Enabled bool
	// MaxActive caps how many watches may poll at once. Watching is
	// budgeted work; an unbounded list would eventually spend every probe.
	MaxActive int
	// Horizon bounds how far ahead a watch may look. State that far out
	// cannot move meaningfully yet.
	Horizon time.Duration
	// Sources names the probes actually registered (flight, sandbox).
	// A candidate whose kind resolves to no registered source is refused
	// honestly rather than watched with a source that cannot answer.
	Sources []string
	// BudgetPerDay is max_watch_checks_per_day: the hard ceiling on
	// probes across every watch in one day.
	BudgetPerDay int
}

// DefaultMaxActive is how many watches may poll at once.
const DefaultMaxActive = 10

// DefaultHorizon is how far ahead a watch may look.
const DefaultHorizon = 45 * 24 * time.Hour

// DefaultBudgetPerDay mirrors proactive.Policy's max_watch_checks_per_day
// default; the agent overrides it with the parsed value.
const DefaultBudgetPerDay = 40

// Default returns the policy with production defaults (everything on).
func Default() Policy {
	return Policy{
		Enabled:      true,
		MaxActive:    DefaultMaxActive,
		Horizon:      DefaultHorizon,
		BudgetPerDay: DefaultBudgetPerDay,
	}
}

// Reason is the machine-readable verdict. Owner-facing text lives in
// render.go; these names are for events, logs and tests.
type Reason string

const (
	ReasonAllowed           Reason = "allowed"
	ReasonProactiveDisabled Reason = "proactive_disabled"
	ReasonSourceUnavailable Reason = "source_unavailable"
	ReasonHorizon           Reason = "event_beyond_horizon"
	ReasonDuplicate         Reason = "duplicate"
	ReasonMaxActive         Reason = "max_active"
	ReasonNoEntity          Reason = "no_entity"
	ReasonPastEvent         Reason = "event_in_past"
	ReasonMissingProvenance Reason = "missing_provenance"
)

// Verdict is the decision plus why.
type Verdict struct {
	Allow  bool
	Reason Reason
	// Detail carries the specifics for an owner-facing explanation.
	Detail string
}

func deny(r Reason, detail string) Verdict { return Verdict{Allow: false, Reason: r, Detail: detail} }

// ResolveSource picks the probe that will answer for this kind, preferring
// the real provider and falling back to the local sandbox file source.
func (p Policy) ResolveSource(kind Kind) (string, bool) {
	has := func(n string) bool {
		for _, s := range p.Sources {
			if s == n {
				return true
			}
		}
		return false
	}
	if kind == KindFlight && has("flight") {
		return "flight", true
	}
	if has("sandbox") {
		return "sandbox", true
	}
	return "", false
}

// Allow is the single gate every creation passes through, automatic or
// explicit, checked in this fixed order: provenance → entity → source →
// time → duplicate → capacity → master switch. Explicit requests skip only
// the master switch and the capacity cap — an owner's direct request is not
// Ghost volunteering.
func (p Policy) Allow(c Candidate, existing []Watch, now time.Time, source string) Verdict {
	if p.MaxActive <= 0 {
		p.MaxActive = DefaultMaxActive
	}
	if p.Horizon <= 0 {
		p.Horizon = DefaultHorizon
	}
	if len(c.Quote) == 0 {
		return deny(ReasonMissingProvenance, "no provenance quote")
	}
	if len(TrimEntity(c.Entity)) == 0 {
		return deny(ReasonNoEntity, "no entity")
	}
	if source == "" {
		_, ok := p.ResolveSource(c.Kind)
		if !ok {
			return deny(ReasonSourceUnavailable,
				fmt.Sprintf("no source is connected for %s", c.Kind))
		}
	}
	if c.EventAt != nil {
		if !c.EventAt.After(now) {
			return deny(ReasonPastEvent, "event already happened")
		}
		if c.EventAt.Sub(now) > p.Horizon {
			return deny(ReasonHorizon, "beyond the watch horizon")
		}
	}
	key := string(c.Kind) + "|" + TrimEntity(c.Entity)
	active := 0
	for _, w := range existing {
		if w.DedupeKey() == key && w.Live() {
			return deny(ReasonDuplicate, w.ID)
		}
		if w.Live() {
			active++
		}
	}
	if !c.Explicit && active >= p.MaxActive {
		return deny(ReasonMaxActive, fmt.Sprintf("%d active", active))
	}
	if !c.Explicit && !p.Enabled {
		return deny(ReasonProactiveDisabled, "proactive preferences disable it")
	}
	return Verdict{Allow: true, Reason: ReasonAllowed}
}

// TrimEntity normalizes an entity for storage and dedupe.
func TrimEntity(s string) string { return normalize(s) }
