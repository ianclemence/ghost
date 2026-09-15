package tools

import (
	"context"
	"testing"
)

type trustedTool struct{ name string }

func (t trustedTool) Name() string                       { return t.name }
func (t trustedTool) Description() string                { return "trusted" }
func (t trustedTool) Parameters() map[string]interface{} { return nil }
func (t trustedTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return &ToolResult{ForLLM: "ok"}
}

type guestTool struct{ trustedTool }

func (t guestTool) Trust() TrustLevel { return TrustUntrusted }

func TestTrustOf(t *testing.T) {
	if got := TrustOf(trustedTool{"host"}); got != TrustTrusted {
		t.Fatalf("host code defaults trusted, got %s", got)
	}
	if got := TrustOf(guestTool{trustedTool{"guest"}}); got != TrustUntrusted {
		t.Fatalf("guest tool must read untrusted, got %s", got)
	}
}

func TestRequireLiveCtx(t *testing.T) {
	if err := RequireLiveCtx(context.Background(), "x"); err != nil {
		t.Fatalf("live ctx must pass: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RequireLiveCtx(ctx, "x"); err == nil {
		t.Fatal("cancelled ctx must refuse launch")
	}
	if err := RequireLiveCtx(nil, "x"); err == nil {
		t.Fatal("nil ctx must refuse launch")
	}
}
