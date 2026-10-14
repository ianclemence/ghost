package agent

import (
	"os"
	"testing"
)

// The always-on core of GHOST.md is read on every turn by every model, so its
// size is a cost and a quality decision. Growing past this cap needs a
// deliberate raise here, in the same change, with per-model results beside it.
const promptCoreMaxBytes = 44000

func TestPromptCoreStaysWithinBudget(t *testing.T) {
	raw, err := os.ReadFile("../../workspace/GHOST.md")
	if err != nil {
		t.Fatal(err)
	}
	core := tieredGhostDoc(string(raw), promptTierCore)
	t.Logf("core bytes: %d, full bytes: %d", len(core), len(raw))
	if len(core) > promptCoreMaxBytes {
		t.Fatalf("always-on prompt core is %d bytes, cap is %d", len(core), promptCoreMaxBytes)
	}
}
