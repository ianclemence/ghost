package tools

import (
	"context"
	"strings"
	"testing"
)

// Updating restarts Ghost, so the tool refuses until the owner's confirmation
// is passed through, and it never falls back to the old git-pull path.
func TestUpdateToolRequiresConfirmation(t *testing.T) {
	res := NewUpdateTool("").Execute(context.Background(), map[string]interface{}{})
	if !res.IsError || !strings.Contains(res.ForLLM, "confirm") {
		t.Fatalf("update without confirm=true must be refused, got %+v", res)
	}
}
