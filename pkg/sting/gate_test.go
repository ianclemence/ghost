package sting

import (
	"testing"
)

func fptr(f float64) *float64 { return &f }

func TestGateNoCallEscalates(t *testing.T) {
	d := Gate("turn on the lights", &CompleteResponse{Type: "call"}, 0.5)
	if !d.Escalate || d.Reason != "no-call" {
		t.Fatalf("expected no-call escalate, got %+v", d)
	}
}

func TestGateNegationDropsCalls(t *testing.T) {
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.99),
		FunctionCalls: []FunctionCall{{Name: "control_lights", Arguments: map[string]interface{}{"room": "study", "action": "on"}}}}
	d := Gate("don't turn on the study lights", resp, 0.5)
	if !d.Escalate || d.Reason != "negated-request" {
		t.Fatalf("expected negated escalate, got %+v", d)
	}
}

func TestGateDedupesIdenticalCalls(t *testing.T) {
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.9),
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	d := Gate("set the thermostat to 22 degrees", resp, 0.5)
	if d.Escalate || len(d.Act) != 1 {
		t.Fatalf("expected 1 deduped act, got %+v", d)
	}
}

func TestGateDropsUngroundedNumbers(t *testing.T) {
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.9),
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(19)}},
		}}
	d := Gate("set the thermostat to 22 degrees", resp, 0.5)
	if !d.Escalate {
		t.Fatalf("expected ungrounded escalate, got %+v", d)
	}
}

func TestGateLowConfidenceEscalates(t *testing.T) {
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.2),
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	d := Gate("set the thermostat to 22 degrees", resp, 0.5)
	if !d.Escalate {
		t.Fatalf("expected low-confidence escalate, got %+v", d)
	}
}

func TestGateNilConfidenceUncalibrated(t *testing.T) {
	resp := &CompleteResponse{Type: "call", // tuned weights: no score
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	d := Gate("set the thermostat to 22 degrees", resp, 0.5)
	if d.Escalate || !d.Uncalibrated || len(d.Act) != 1 {
		t.Fatalf("expected uncalibrated act, got %+v", d)
	}
}

func TestGateWithLedgerBelowTargetEscalates(t *testing.T) {
	// positive-single prior is 0.84 < 0.9: auto-act off, even at 0.99.
	tr := NewTracker(DefaultPriors())
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.99),
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	d := GateWithLedger("set the thermostat to 22 degrees", resp, tr, 0.5)
	if !d.Escalate {
		t.Fatalf("expected below-target escalate, got %+v", d)
	}
	if d.Scope != ScopePositiveSingle {
		t.Fatalf("expected positive-single scope, got %q", d.Scope)
	}
}

func TestGateWithLedgerUnscoredRequiresMeasuredScope(t *testing.T) {
	resp := &CompleteResponse{Type: "call", // tuned weights: no score
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	// A matured, at-target scope may act on measured precision alone.
	tr := NewTracker([]Prior{{Scope: ScopePositiveSingle, N: 30, Correct: 29, Source: "test"}})
	d := GateWithLedger("set the thermostat to 22 degrees", resp, tr, 0.5)
	if d.Escalate || !d.Uncalibrated || len(d.Act) != 1 {
		t.Fatalf("expected ledger-calibrated unscored act, got %+v", d)
	}
	// An unmeasured scope must not.
	empty := NewTracker(nil)
	if d := GateWithLedger("set the thermostat to 22 degrees", resp, empty, 0.5); !d.Escalate {
		t.Fatalf("expected unscored escalate on unmeasured scope, got %+v", d)
	}
}

func TestGateWithLedgerThinDataRaisesThreshold(t *testing.T) {
	// Thin data raises the bar by 0.2 (base 0.5 -> 0.7): 0.6 escalates.
	tr := NewTracker(nil)
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.6),
		FunctionCalls: []FunctionCall{
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(22)}},
		}}
	if d := GateWithLedger("set the thermostat to 22 degrees", resp, tr, 0.5); !d.Escalate {
		t.Fatalf("expected thin-data escalate at 0.6, got %+v", d)
	}
	resp.Confidence = fptr(0.8)
	if d := GateWithLedger("set the thermostat to 22 degrees", resp, tr, 0.5); d.Escalate {
		t.Fatalf("expected thin-data act at 0.8, got %+v", d)
	}
}

func TestGateParallelPreserved(t *testing.T) {
	resp := &CompleteResponse{Type: "call", Confidence: fptr(0.9),
		FunctionCalls: []FunctionCall{
			{Name: "control_lights", Arguments: map[string]interface{}{"room": "bedroom", "action": "off"}},
			{Name: "set_thermostat", Arguments: map[string]interface{}{"temperature": float64(18)}},
		}}
	d := Gate("turn off the bedroom lights and set the thermostat to 18 degrees", resp, 0.5)
	if d.Escalate || len(d.Act) != 2 {
		t.Fatalf("expected 2 parallel acts, got %+v", d)
	}
}

func TestSubsetCapsAndOrders(t *testing.T) {
	got := Subset(
		[]string{"memory_recall", "web_search", "exec", "weather_now"},
		[]string{"web_search", "weather_now", "memory_recall", "exec"},
		2,
	)
	if len(got) != 2 || got[0] != "web_search" || got[1] != "weather_now" {
		t.Fatalf("unexpected subset %v", got)
	}
}

func TestValidateSubsetRejectsOversize(t *testing.T) {
	if err := ValidateSubset([]string{"a", "b", "c", "d", "e", "f"}); err == nil {
		t.Fatal("expected oversize error")
	}
	if err := ValidateSubset(nil); err == nil {
		t.Fatal("expected empty error")
	}
}
