package agent

import (
	"os"
	"strings"
	"testing"
)

// Context tiering is the single largest prompt reduction in this pass, and its
// one non-negotiable property is that nothing governing behaviour or authority
// is dropped. That property is checked here against the shipped GHOST.md
// directly, rather than inferred from a prompt score.
func TestPromptCoreRetainsBehaviourAndAuthority(t *testing.T) {
	raw, err := os.ReadFile("../../workspace/GHOST.md")
	if err != nil {
		t.Skipf("workspace GHOST.md unavailable: %v", err)
	}
	full := string(raw)
	core := tieredGhostDoc(full, promptTierCore)

	mustKeep := []string{
		"# Foundational Invariants",
		"# How Intent Becomes Outcome",
		"# Identity",
		"# Authority and Permissions",
		"## How approval works",
		"# Execution and Evidence",
		"## Thinking is not doing",
		"## Two kinds of truth",
		"## Canonical truth",
		"# Memory",
		"# Routines",
		"# Time",
		"# Personality",
		"# Interacting",
		"# Safety",
		"# Failure and Recovery",
		"# Final Rules",
	}
	for _, want := range mustKeep {
		if !strings.Contains(core, want) {
			t.Errorf("behavioural core is missing %q", want)
		}
	}

	// Check the section HEADING, not a bare substring: retained sections
	// legitimately cross-reference the reference material by name.
	mustDrop := []string{
		"\n# Tools — the operating manual\n",
		"\n# Credentials and Setup\n",
		"\n# Channels and surfaces\n",
	}
	for _, gone := range mustDrop {
		if strings.Contains(core, gone) {
			t.Errorf("operational reference %q must not ship in the core", gone)
		}
		heading := strings.Trim(gone, "\n")
		// It must still exist on demand — dropping it from the prompt is not
		// deleting it from the workspace.
		if !strings.Contains(full, heading) {
			t.Errorf("reference %q must remain readable in GHOST.md", heading)
		}
	}

	// The core must point the model at the detail it no longer carries.
	if !strings.Contains(core, "read_file") {
		t.Error("the core must tell the model where the operational reference lives")
	}
	if len(core) >= len(full) {
		t.Fatalf("tiering did not reduce anything: %d -> %d bytes", len(full), len(core))
	}
	if saved := 100 * (len(full) - len(core)) / len(full); saved < 25 {
		t.Errorf("expected a material reduction, got %d%%", saved)
	}
	t.Logf("GHOST.md %d -> %d bytes (%.0f%% smaller)", len(full), len(core), 100*float64(len(full)-len(core))/float64(len(full)))
}

// The full tier still carries everything, so nothing is unreadable to a caller
// that asks for it.
func TestPromptFullTierKeepsEverything(t *testing.T) {
	raw, err := os.ReadFile("../../workspace/GHOST.md")
	if err != nil {
		t.Skipf("workspace GHOST.md unavailable: %v", err)
	}
	full := string(raw)
	if got := tieredGhostDoc(full, promptTierFull); got != full {
		t.Fatal("the full tier must return the document unchanged")
	}
}
