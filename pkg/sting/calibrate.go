package sting

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Calibration is the ledger-backed authority the gate names.
//
// The upstream engine ships a calibrated head for its base model — but
// fine-tuning freezes it (tuned weights report no score) and our Pi
// spike measured real per-category gaps (missing slots route at ~25%).
// So Ghost keeps its own empirical reliability ledger: measured
// precision per routing scope, mapped to an act/escalate threshold. The
// ledger is not a report; GateWithLedger consults it on every turn and
// it decides, including when the engine reports no score at all.
//
// Rules (deliberately blunt):
//   - thin data (n < MinSamples): conservative threshold, marked thin-data.
//   - precision >= TargetPrecision: the configured base threshold applies.
//   - below target: threshold 1.01 — auto-act disabled for that scope
//     until measured data improves. Escalation, not wrong execution.
//   - an unscored turn (tuned weights) acts only for a scope the ledger
//     has measured at or above target; otherwise it escalates.
//
// The ledger persists as JSON (Save/Load) so harness runs
// (golden, suite replays) ratchet it forward. Priors ship from our
// measured Pi spike and are labeled as such — small-n, replaceable.
const (
	// MinSamples below which a scope is thin-data by definition.
	MinSamples = 10
	// TargetPrecision is the auto-act bar: 9/10 routed turns correct.
	TargetPrecision = 0.9
	// DisabledThreshold exceeds any possible confidence: always escalate.
	DisabledThreshold = 1.01
)

// Serving scopes. The gate only ever sees a turn that produced at least
// one validated call, so the shape of that call set is what the ledger
// can measure live. The names match DefaultPriors so the Pi-spike
// measurements apply directly; suite:* scopes stay harness-only.
const (
	// ScopePositiveSingle is a turn that validated to exactly one call.
	ScopePositiveSingle = "positive-single"
	// ScopeParallel is a turn that validated to more than one call.
	ScopeParallel = "parallel"
)

// ScopeFor classifies a validated call set into the serving scope the
// ledger is keyed by.
func ScopeFor(calls int) string {
	if calls > 1 {
		return ScopeParallel
	}
	return ScopePositiveSingle
}

// AllowsAutoAct reports whether a scope has earned the right to act
// without an engine confidence score (the tuned-weights case). Only
// measured, at-target scopes qualify; a nil ledger authorizes nothing.
func (t *Tracker) AllowsAutoAct(scope string) bool {
	if t == nil {
		return false
	}
	p, n := t.Precision(scope)
	return n >= MinSamples && p >= TargetPrecision
}

// Ledger resolves the configured ledger path into the authority the gate
// consults:
//   - "" → the shipped priors (default);
//   - "off" → nil, restoring the pre-ledger confidence-only behaviour;
//   - a path → that saved ledger.
//
// A file that does not exist yet yields the shipped priors (the starting
// point); a file that exists but cannot be read or parsed fails closed to
// an empty ledger, so every scope is thin-data rather than silently
// trusted.
func Ledger(path string) *Tracker {
	switch {
	case strings.EqualFold(strings.TrimSpace(path), "off"):
		return nil
	case strings.TrimSpace(path) == "":
		return NewTracker(DefaultPriors())
	default:
		tr, err := LoadTracker(strings.TrimSpace(path))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return NewTracker(DefaultPriors())
			}
			return NewTracker(nil)
		}
		return tr
	}
}

// Prior is one measured scope outcome.
type Prior struct {
	Scope   string `json:"scope"`
	N       int    `json:"n"`
	Correct int    `json:"correct"`
	Source  string `json:"source"`
}

// DefaultPriors are suite-level measurements from the Pi spike (base
// weights, smart_home + productivity suites). Small-n and labeled —
// the flywheel replaces them with device-measured data over time.
func DefaultPriors() []Prior {
	return []Prior{
		{Scope: ScopePositiveSingle, N: 19, Correct: 16, Source: "pi-spike smart_home run1"},
		{Scope: "missing-slot", N: 4, Correct: 1, Source: "pi-spike smart_home run1"},
		{Scope: "negation", N: 3, Correct: 3, Source: "pi-spike smart_home run1"},
		{Scope: "invalid", N: 2, Correct: 2, Source: "pi-spike smart_home run1"},
		{Scope: "irrelevant", N: 3, Correct: 3, Source: "pi-spike smart_home run1"},
		{Scope: ScopeParallel, N: 2, Correct: 1, Source: "pi-spike smart_home run1"},
		{Scope: "suite:smart_home", N: 64, Correct: 51, Source: "pi-spike runs 1-2 (25+26/32)"},
		{Scope: "suite:productivity", N: 32, Correct: 29, Source: "pi-spike run1"},
	}
}

// Tracker accumulates scope outcomes over priors + live records.
type Tracker struct {
	n       map[string]int
	correct map[string]int
	sources map[string]string
}

// NewTracker seeds a ledger from priors.
func NewTracker(priors []Prior) *Tracker {
	t := &Tracker{n: map[string]int{}, correct: map[string]int{}, sources: map[string]string{}}
	for _, p := range priors {
		t.n[p.Scope] += p.N
		t.correct[p.Scope] += p.Correct
		if p.Source != "" {
			t.sources[p.Scope] = p.Source
		}
	}
	return t
}

// Record adds one observed routing turn.
func (t *Tracker) Record(scope string, correct bool) {
	if t.n == nil {
		t.n = map[string]int{}
		t.correct = map[string]int{}
	}
	t.n[scope]++
	if correct {
		t.correct[scope]++
	}
}

// Precision returns measured precision and sample count for a scope.
// Unknown scopes report (0, 0): thin-data by definition.
func (t *Tracker) Precision(scope string) (float64, int) {
	n := t.n[scope]
	if n == 0 {
		return 0, 0
	}
	return float64(t.correct[scope]) / float64(n), n
}

// ThresholdFor maps a scope to its act/escalate threshold given the
// configured base. Returns the threshold and a short reason.
func (t *Tracker) ThresholdFor(scope string, base float64) (float64, string) {
	p, n := t.Precision(scope)
	if n < MinSamples {
		capped := base + 0.2
		if capped > 0.95 {
			capped = 0.95
		}
		return capped, "thin-data"
	}
	if p >= TargetPrecision {
		return base, "calibrated"
	}
	return DisabledThreshold, fmt.Sprintf("below-target:%.2f", p)
}

// Scopes lists every scope in the ledger, sorted.
func (t *Tracker) Scopes() []string {
	var out []string
	for s := range t.n {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (t *Tracker) Save(path string) error {
	var priors []Prior
	for scope, n := range t.n {
		priors = append(priors, Prior{Scope: scope, N: n, Correct: t.correct[scope], Source: t.sources[scope]})
	}
	raw, err := json.MarshalIndent(priors, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

// LoadTracker reads a ledger file written by Save.
func LoadTracker(path string) (*Tracker, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var priors []Prior
	if err := json.Unmarshal(raw, &priors); err != nil {
		return nil, err
	}
	return NewTracker(priors), nil
}
