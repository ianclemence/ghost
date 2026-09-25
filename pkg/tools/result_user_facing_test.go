package tools

import (
	"strings"
	"testing"
)

// A failed provider completion carries model-only guidance in ForLLM.
// UserFacing must strip it: deterministic paths answer the owner directly,
// with no model in between to interpret guidance aimed at a model.
// Regression: the raw "(completion: failed; do not present fabricated
// data)" suffix reached the chat verbatim on weather failures.
func TestUserFacingStripsModelGuidance(t *testing.T) {
	const product = "The network request failed. I'll try again shortly."
	res := providerError(product)
	if !strings.Contains(res.ForLLM, "do not present fabricated data") {
		t.Fatalf("ForLLM must keep the guidance for the model path: %q", res.ForLLM)
	}
	got := res.UserFacing()
	if got != product {
		t.Fatalf("UserFacing = %q, want %q", got, product)
	}
	if strings.Contains(got, "completion: failed") {
		t.Fatalf("UserFacing leaked model guidance: %q", got)
	}
}

// ForUser, when set, is the owner-facing text and wins outright.
func TestUserFacingPrefersForUser(t *testing.T) {
	res := &ToolResult{
		ForLLM:  "llm text (completion: failed; do not present fabricated data)",
		ForUser: "owner text",
	}
	if got := res.UserFacing(); got != "owner text" {
		t.Fatalf("UserFacing = %q, want %q", got, "owner text")
	}
}

func TestUserFacingNilSafe(t *testing.T) {
	var res *ToolResult
	if got := res.UserFacing(); got != "" {
		t.Fatalf("UserFacing(nil) = %q, want empty", got)
	}
}
