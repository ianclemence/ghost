package tools

import (
	"context"
	"strings"
	"testing"
)

type leakyTool struct{ out string }

func (leakyTool) Name() string        { return "leaky" }
func (leakyTool) Description() string { return "echoes" }
func (leakyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (t leakyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	r := NewToolResult(t.out)
	r.ForUser = t.out
	r.Evidence = map[string]interface{}{"page": t.out, "nested": []interface{}{t.out}}
	return r
}

// A password that matches no credential shape must still never leave a tool.
func TestRegistryScrubsVaultSecrets(t *testing.T) {
	defer SetSecretSource(func() []string { return []string{"correct-horse-7", "hunter22"} })()
	reg := NewToolRegistry()
	reg.Register(leakyTool{out: "logged in as sam with correct-horse-7 ok; also hunter22"})
	res := reg.Execute(context.Background(), "leaky", map[string]interface{}{})
	for name, got := range map[string]string{
		"ForLLM": res.ForLLM, "ForUser": res.ForUser,
		"evidence": res.Evidence["page"].(string), "nested": res.Evidence["nested"].([]interface{})[0].(string),
	} {
		if strings.Contains(got, "correct-horse-7") || strings.Contains(got, "hunter22") {
			t.Fatalf("%s leaked a vault secret: %q", name, got)
		}
		if !strings.Contains(got, secretMarker) {
			t.Fatalf("%s should carry the removal marker: %q", name, got)
		}
	}
}

func TestScrubSecretsLongestFirst(t *testing.T) {
	got := ScrubSecrets("pw=abcdef-long", []string{"abcdef-long", "abcdef"})
	if strings.Contains(got, "long") {
		t.Fatalf("shorter secret masked first, leaving a tail: %q", got)
	}
}

func TestScrubLeavesOrdinaryOutputAlone(t *testing.T) {
	defer SetSecretSource(func() []string { return nil })()
	if got := ScrubSecrets("nothing to see", nil); got != "nothing to see" {
		t.Fatal(got)
	}
}
