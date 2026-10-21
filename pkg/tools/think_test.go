package tools

import (
	"context"
	"strings"
	"testing"
)

func TestThinkToolDoesNothingButAcknowledge(t *testing.T) {
	tool := NewThinkTool()
	res := tool.Execute(context.Background(), map[string]interface{}{"thought": "rule: confirm before sending; I have the address"})
	if res.IsError || !res.Silent {
		t.Fatalf("a thought is silent and not an error: %+v", res)
	}
	if bad := tool.Execute(context.Background(), map[string]interface{}{"thought": "  "}); !bad.IsError {
		t.Fatal("an empty thought must be refused")
	}
	long := tool.Execute(context.Background(), map[string]interface{}{"thought": strings.Repeat("x", 5000)})
	if long.IsError || len(long.ForLLM) > 200 {
		t.Fatalf("a long thought is acknowledged briefly, never echoed back: %d", len(long.ForLLM))
	}
}
