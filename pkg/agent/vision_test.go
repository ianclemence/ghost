package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestVisionModelFor(t *testing.T) {
	// deepseek-flash (V4.1-Flash) is multimodal: no remap needed.
	if vm := visionModelFor("deepseek:deepseek-flash"); vm != "" {
		t.Errorf("multimodal deepseek-flash should be left as-is, got %q", vm)
	}
	// A non-vision DeepSeek model routes to the vision-capable Flash.
	if vm := visionModelFor("deepseek:deepseek-v4-pro"); vm != "deepseek:deepseek-flash" {
		t.Errorf("got %q, want deepseek:deepseek-flash", vm)
	}
	if vm := visionModelFor("openai:gpt-4o"); vm != "" {
		t.Errorf("openai should be left as-is, got %q", vm)
	}
}

func TestMessagesContainImages(t *testing.T) {
	if messagesContainImages([]providers.Message{{Role: "user", Content: "hi"}}) {
		t.Error("expected false for text-only")
	}
	withImg := []providers.Message{{Role: "user", Content: "look", MultiContent: []providers.ContentPart{
		{Type: "image_url", ImageURL: &providers.ImageURL{URL: "data:image/png;base64,abc"}},
	}}}
	if !messagesContainImages(withImg) {
		t.Error("expected true for image message")
	}
}

func TestIsLocalModel(t *testing.T) {
	al := &AgentLoop{model: "ollama:qwen3:0.6b"}
	if !al.isLocalModel() {
		t.Error("expected ollama to be local")
	}
	al2 := &AgentLoop{model: "deepseek:deepseek-flash"}
	if al2.isLocalModel() {
		t.Error("expected deepseek to be cloud")
	}
}

func TestLearningsSummaryNilEvolution(t *testing.T) {
	al := &AgentLoop{}
	l := al.LearningsSummary()
	if l["records"].(int) != 0 || l["drafts"].(int) != 0 {
		t.Fatalf("expected empty learnings summary, got %+v", l)
	}
}
