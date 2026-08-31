package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureQuickNote(t *testing.T) {
	dir := t.TempDir()
	al := &AgentLoop{workspace: dir}

	// A clear capture directive persists.
	al.captureQuickNote("Remember this: ship the onboarding email on Friday", "mobile")
	data, err := os.ReadFile(filepath.Join(dir, "data", "captures.md"))
	if err != nil {
		t.Fatalf("expected captures.md created: %v", err)
	}
	if !strings.Contains(string(data), "ship the onboarding email on Friday") {
		t.Fatalf("expected captured content, got: %s", string(data))
	}

	// Non-directive messages do not create a capture.
	al.captureQuickNote("What is the weather like?", "mobile")
	// A declaration-style directive is left to the extractor, not captured raw.
	al.captureQuickNote("remember that I prefer tea", "mobile")
	data2, _ := os.ReadFile(filepath.Join(dir, "data", "captures.md"))
	if strings.Contains(string(data2), "prefer tea") {
		t.Fatalf("declaration-style directive should not be captured as text: %s", string(data2))
	}
	if strings.Count(string(data2), "## ") != 1 {
		t.Fatalf("expected exactly one capture, got: %s", string(data2))
	}
}
