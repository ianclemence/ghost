package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestVisionModelFor(t *testing.T) {
	if vm := visionModelFor("deepseek:deepseek-v4-flash"); vm != "deepseek:deepseek-v4-flash-vision-exp" {
		t.Errorf("got %q", vm)
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
	al2 := &AgentLoop{model: "deepseek:deepseek-v4-flash"}
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
