package tools

import (
	"context"
	"strings"
	"testing"
)

// A stub with one required property: the shape the SACCO canvas calls
// failed against six times (everything nested in one "raw" string).
type envelopeTestTool struct {
	last map[string]interface{}
}

func (t *envelopeTestTool) Name() string { return "envelope_test_tool" }
func (t *envelopeTestTool) Description() string { return "test tool" }
func (t *envelopeTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{"type": "string"},
			"html":  map[string]interface{}{"type": "string"},
		},
		"required": []string{"title"},
	}
}
func (t *envelopeTestTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	t.last = args
	return NewToolResult("ok")
}

// A stub that legitimately takes raw input: the envelope must not fire.
type rawKeepingTestTool struct{ envelopeTestTool }

func (t *rawKeepingTestTool) Name() string { return "raw_keeping_test_tool" }
func (t *rawKeepingTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"raw":   map[string]interface{}{"type": "string"},
			"title": map[string]interface{}{"type": "string"},
		},
		"required": []string{"title"},
	}
}

func execAs(reg *ToolRegistry, name string, args map[string]interface{}) *ToolResult {
	return reg.ExecuteWithContext(context.Background(), name, args, "test", "test", "sess-envelope", nil)
}

// A lone raw JSON string carrying the documented fields executes as the
// fields: the six-fold "missing properties: title" loop cannot recur.
func TestRawEnvelopeUnwraps(t *testing.T) {
	reg := NewToolRegistry()
	stub := &envelopeTestTool{}
	reg.Register(stub)
	res := execAs(reg, "envelope_test_tool", map[string]interface{}{
		"raw": `{"title": "SACCO Loan Book", "html": "<!doctype html>"}`,
	})
	if res.IsError {
		t.Fatalf("enveloped call rejected: %s", res.ForLLM)
	}
	if stub.last["title"] != "SACCO Loan Book" || stub.last["html"] != "<!doctype html>" {
		t.Fatalf("fields not unwrapped: %+v", stub.last)
	}
	if _, ok := stub.last["raw"]; ok {
		t.Fatalf("raw key leaked through: %+v", stub.last)
	}
}

func TestRawEnvelopeLeavesOthersAlone(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&envelopeTestTool{})
	reg.Register(&rawKeepingTestTool{})
	// Not JSON: untouched, still invalid.
	if res := execAs(reg, "envelope_test_tool", map[string]interface{}{"raw": "hello"}); !res.IsError {
		t.Fatal("non-JSON raw must stay invalid")
	}
	// Extra keys beside raw: untouched, still invalid (title missing).
	if res := execAs(reg, "envelope_test_tool", map[string]interface{}{"summary": "x", "raw": `{"title": "Y"}`}); !res.IsError {
		t.Fatal("multi-key args must not unwrap")
	}
	// A tool owning a raw property: untouched, still invalid (title missing).
	if res := execAs(reg, "raw_keeping_test_tool", map[string]interface{}{"raw": `{"title": "X"}`}); !res.IsError {
		t.Fatal("a tool's own raw property must never unwrap")
	}
	// Empty envelope: untouched.
	if res := execAs(reg, "envelope_test_tool", map[string]interface{}{"raw": "  "}); !res.IsError {
		t.Fatal("empty raw must stay invalid")
	}
}

// The third identical validation failure tells the model to reshape the
// call instead of retrying; a success in between resets the count.
func TestInvalidRepeatBreaksTheLoop(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&envelopeTestTool{})
	bad := map[string]interface{}{"html": "<p>no title</p>"}
	for i := 1; i <= 2; i++ {
		res := execAs(reg, "envelope_test_tool", bad)
		if !res.IsError {
			t.Fatalf("attempt %d must fail", i)
		}
		if strings.Contains(res.ForLLM, "failed validation") {
			t.Fatalf("attempt %d must not yet nudge:\n%s", i, res.ForLLM)
		}
	}
	res := execAs(reg, "envelope_test_tool", bad)
	if !res.IsError || !strings.Contains(res.ForLLM, "failed validation 3 times") {
		t.Fatalf("third repeat must break the loop:\n%s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "top level") {
		t.Fatalf("nudge must say where the properties go:\n%s", res.ForLLM)
	}
	// A valid call resets the count: the next failure starts over quietly.
	if ok := execAs(reg, "envelope_test_tool", map[string]interface{}{"title": "X"}); ok.IsError {
		t.Fatalf("valid call failed: %s", ok.ForLLM)
	}
	again := execAs(reg, "envelope_test_tool", bad)
	if !again.IsError || strings.Contains(again.ForLLM, "failed validation") {
		t.Fatalf("count must reset after success:\n%s", again.ForLLM)
	}
}

// The canvas brief now shows the exact call shape, so "a shape I couldn't
// get accepted" has an answer in the prompt itself.
func TestCanvasDescriptionShowsCallShape(t *testing.T) {
	desc := (&CanvasTool{}).Description()
	for _, want := range []string{`"title"`, "top level", "single string"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("canvas description missing %q", want)
		}
	}
}
