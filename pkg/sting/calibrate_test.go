package sting

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThresholdThinDataConservative(t *testing.T) {
	tr := NewTracker(nil)
	th, reason := tr.ThresholdFor("missing-slot", 0.5)
	if reason != "thin-data" || th != 0.7 {
		t.Fatalf("expected conservative 0.7/thin-data, got %.2f/%s", th, reason)
	}
}

func TestThresholdCalibratedScope(t *testing.T) {
	tr := NewTracker([]Prior{{Scope: "negation", N: 30, Correct: 28, Source: "test"}})
	th, reason := tr.ThresholdFor("negation", 0.5)
	if reason != "calibrated" || th != 0.5 {
		t.Fatalf("expected base/calibrated, got %.2f/%s", th, reason)
	}
}

func TestThresholdDisablesWeakScope(t *testing.T) {
	// The headline prior: missing slots route at ~25% — auto-act off.
	tr := NewTracker(DefaultPriors())
	// missing-slot n=4 < MinSamples → thin-data conservative, not disabled;
	// simulate a matured weak scope instead.
	matured := NewTracker([]Prior{{Scope: "x", N: 40, Correct: 10, Source: "test"}})
	th, reason := matured.ThresholdFor("x", 0.5)
	if th != DisabledThreshold {
		t.Fatalf("expected disabled threshold, got %.2f/%s", th, reason)
	}
	_ = tr
}

func TestDefaultPriorsMissingSlotWeak(t *testing.T) {
	tr := NewTracker(DefaultPriors())
	p, n := tr.Precision("missing-slot")
	if n != 4 || p != 0.25 {
		t.Fatalf("expected 0.25/4, got %.2f/%d", p, n)
	}
}

func TestAllowsAutoActOnlyForMeasuredTargetScopes(t *testing.T) {
	if (*Tracker)(nil).AllowsAutoAct("x") {
		t.Fatal("nil tracker must authorize nothing")
	}
	thin := NewTracker([]Prior{{Scope: "x", N: 5, Correct: 5, Source: "test"}})
	if thin.AllowsAutoAct("x") {
		t.Fatal("thin data must not auto-act")
	}
	weak := NewTracker([]Prior{{Scope: "x", N: 30, Correct: 20, Source: "test"}})
	if weak.AllowsAutoAct("x") {
		t.Fatal("below-target must not auto-act")
	}
	good := NewTracker([]Prior{{Scope: "x", N: 30, Correct: 28, Source: "test"}})
	if !good.AllowsAutoAct("x") {
		t.Fatal("measured at-target scope must auto-act")
	}
}

func TestLedgerResolver(t *testing.T) {
	if tr := Ledger("off"); tr != nil {
		t.Fatal("off must disable the ledger")
	}
	if tr := Ledger(""); tr == nil || len(tr.Scopes()) == 0 {
		t.Fatal("empty path must yield the shipped priors")
	}
	// A not-yet-written ledger is the shipped priors, not a trust failure.
	if tr := Ledger(filepath.Join(t.TempDir(), "missing.json")); tr == nil || len(tr.Scopes()) == 0 {
		t.Fatal("missing ledger must fall back to the shipped priors")
	}
	// A corrupt ledger fails closed: no scope is trusted.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if tr := Ledger(bad); tr == nil || len(tr.Scopes()) != 0 {
		t.Fatal("corrupt ledger must fail closed to an empty tracker")
	}
}

func TestTrackerRecordAndSaveLoad(t *testing.T) {
	tr := NewTracker(nil)
	tr.Record("s", true)
	tr.Record("s", false)
	if p, n := tr.Precision("s"); n != 2 || p != 0.5 {
		t.Fatalf("got %.2f/%d", p, n)
	}
	path := filepath.Join(t.TempDir(), "stingcal.json")
	if err := tr.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, n := loaded.Precision("s"); n != 2 || p != 0.5 {
		t.Fatalf("round-trip got %.2f/%d", p, n)
	}
}
