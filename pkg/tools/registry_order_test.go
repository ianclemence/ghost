package tools

import (
	"fmt"
	"strings"
	"testing"
)

// Tool definitions open every prompt, so their order must be stable:
// providers cache prompt prefixes (Anthropic explicitly, DeepSeek and
// OpenAI automatically) and a reshuffled tool list invalidates the cache
// on every call. Map iteration made the order random.
func TestToolDefinitionsDeterministicOrder(t *testing.T) {
	r := NewToolRegistry()
	for i := 0; i < 40; i++ {
		r.Register(&stubTool{name: fmt.Sprintf("tool_%02d", 39-i)})
	}
	first := names(r)
	for i := 0; i < 20; i++ {
		if got := names(r); got != first {
			t.Fatalf("tool order changed between calls:\n%s\n%s", first, got)
		}
	}
	if !strings.HasPrefix(first, "tool_00,tool_01,") {
		t.Fatalf("want sorted order, got %s", first)
	}
	var schemaNames []string
	for _, d := range r.GetDefinitions() {
		schemaNames = append(schemaNames, d["function"].(map[string]interface{})["name"].(string))
	}
	if strings.Join(schemaNames, ",") != first {
		t.Fatal("GetDefinitions must use the same stable order")
	}
}

func names(r *ToolRegistry) string {
	var out []string
	for _, d := range r.ToProviderDefs() {
		out = append(out, d.Function.Name)
	}
	return strings.Join(out, ",")
}
