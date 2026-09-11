package tools

import (
	"context"
	"testing"
)

type evidenceStub struct {
	name     string
	evidence map[string]interface{}
}

func (e *evidenceStub) Name() string        { return e.name }
func (e *evidenceStub) Description() string { return "stub" }
func (e *evidenceStub) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (e *evidenceStub) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return &ToolResult{ForLLM: "ok", Evidence: e.evidence}
}

// A capability whose contract requires evidence must not report success
// without it.
func TestRegistryEnforcesEvidenceContract(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&evidenceStub{name: "message"}) // message.send requires acknowledgement
	res := r.ExecuteWithContext(context.Background(), "message", nil, "cli", "direct", "s1", nil)
	if !res.IsError {
		t.Fatal("success without required evidence must be refused")
	}
}

// Evidence-backed success passes.
func TestRegistryAcceptsEvidenceBackedSuccess(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&evidenceStub{name: "message", evidence: MessageEvidence("cli", "direct", "hi")})
	res := r.ExecuteWithContext(context.Background(), "message", nil, "cli", "direct", "s1", nil)
	if res.IsError {
		t.Fatalf("evidence-backed success must pass, got %q", res.ForLLM)
	}
}

// Read-only capabilities are unaffected by the evidence contract.
func TestRegistryReadOnlyNeedsNoEvidence(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&evidenceStub{name: "read_file"})
	if res := r.ExecuteWithContext(context.Background(), "read_file", nil, "", "", "", nil); res.IsError {
		t.Fatalf("read-only must not require evidence, got %q", res.ForLLM)
	}
}
