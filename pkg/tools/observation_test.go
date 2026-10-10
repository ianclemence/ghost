package tools

import (
	"testing"
)

// The artifact a publish created must survive normalization so the
// canonical event can name it and a later deletion can retract exactly it.
func TestNewObservationExtractsArtifactID(t *testing.T) {
	res := &ToolResult{
		ForLLM:   "Published artifact art-123 (file: Q3 report). It is now available to the user.",
		Evidence: ArtifactEvidence("art-123", "file", "Q3 report"),
	}
	o := NewObservation("publish_artifact", res)
	if o.ArtifactID != "art-123" {
		t.Fatalf("ArtifactID = %q, want art-123", o.ArtifactID)
	}
}

func TestNewObservationWithoutArtifactEvidence(t *testing.T) {
	o := NewObservation("web_search", &ToolResult{ForLLM: "results"})
	if o.ArtifactID != "" {
		t.Fatalf("ArtifactID = %q, want empty when no artifact evidence", o.ArtifactID)
	}
}
