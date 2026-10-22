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

// The owner asks "is there an update?" on their phone. The tool must be in the
// mobile profile and the question must route to it; otherwise Ghost answers
// that it cannot check, which is what happened before this test existed.
func TestUpdateIsOfferedForUpdateQuestionsOnMobile(t *testing.T) {
	if !ProfileMobileSafe.Allows("update") {
		t.Fatal("the update tool must be available on the mobile surface")
	}
	reg := NewToolRegistry()
	reg.Register(NewUpdateTool(t.TempDir()))
	for _, msg := range []string{
		"check for an update",
		"is there an update?",
		"any updates available?",
		"what version are you running?",
		"update yourself",
	} {
		got := FilterToolsForTurn(reg, ProfileMobileSafe, msg, false)
		if _, ok := got.Get("update"); !ok {
			t.Errorf("update not offered for %q", msg)
		}
	}
	if got := FilterToolsForTurn(reg, ProfileMobileSafe, "what's the weather", false); got != nil {
		if _, ok := got.Get("update"); ok {
			t.Error("update should not be offered for unrelated chat")
		}
	}
}

func TestUpdateToolOffersAReadOnlyCheck(t *testing.T) {
	props, _ := NewUpdateTool("").Parameters()["properties"].(map[string]interface{})
	if _, ok := props["check"]; !ok {
		t.Fatal("update must expose check=true so a version question changes nothing")
	}
	if _, ok := props["confirm"]; !ok {
		t.Fatal("update must still require confirm=true to install")
	}
}
