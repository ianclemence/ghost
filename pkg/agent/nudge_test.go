package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func setupTestNudgeManager(memoryInterval, skillInterval int) *NudgeManager {
	cfg := NudgeConfig{
		Enabled:        true,
		MemoryInterval: memoryInterval,
		SkillInterval:  skillInterval,
	}
	return NewNudgeManager(cfg, nil)
}

func TestNudgeManagerDisabled(t *testing.T) {
	nm := NewNudgeManager(NudgeConfig{Enabled: false}, nil)
	nm.OnUserTurn("session1")
	if nm.ShouldReviewMemory() {
		t.Fatal("should not review memory when disabled")
	}
	if nm.ShouldReviewSkills() {
		t.Fatal("should not review skills when disabled")
	}
}

func TestNudgeManagerMemoryInterval(t *testing.T) {
	nm := setupTestNudgeManager(3, 10)
	nm.OnUserTurn("session1")
	nm.OnUserTurn("session1")
	if nm.ShouldReviewMemory() {
		t.Fatal("should not review memory before threshold")
	}
	nm.OnUserTurn("session1")
	if !nm.ShouldReviewMemory() {
		t.Fatal("should review memory at threshold")
	}
}

func TestNudgeManagerSkillInterval(t *testing.T) {
	nm := setupTestNudgeManager(20, 3)
	nm.OnToolIteration("session1")
	nm.OnToolIteration("session1")
	if nm.ShouldReviewSkills() {
		t.Fatal("should not review skills before threshold")
	}
	nm.OnToolIteration("session1")
	if !nm.ShouldReviewSkills() {
		t.Fatal("should review skills at threshold")
	}
}

func TestNudgeManagerMemoryReset(t *testing.T) {
	nm := setupTestNudgeManager(3, 10)
	nm.OnUserTurn("session1")
	nm.OnUserTurn("session1")
	nm.OnMemoryToolUsed("session1")
	if nm.GetTurnCount() != 0 {
		t.Fatal("turn count should reset after memory tool use")
	}
}

func TestNudgeManagerSkillReset(t *testing.T) {
	nm := setupTestNudgeManager(20, 3)
	nm.OnToolIteration("session1")
	nm.OnToolIteration("session1")
	nm.OnSkillToolUsed("session1")
	if nm.GetToolIterCount() != 0 {
		t.Fatal("tool iter count should reset after skill tool use")
	}
}

func TestNudgeManagerReset(t *testing.T) {
	nm := setupTestNudgeManager(3, 3)
	nm.OnUserTurn("session1")
	nm.OnToolIteration("session1")
	nm.Reset()
	if nm.GetTurnCount() != 0 || nm.GetToolIterCount() != 0 {
		t.Fatal("counts should be zero after reset")
	}
}

func TestNudgeManagerShouldReviewMemoryResetsCounter(t *testing.T) {
	nm := setupTestNudgeManager(2, 10)
	nm.OnUserTurn("session1")
	nm.OnUserTurn("session1")
	nm.ShouldReviewMemory()
	if nm.GetTurnCount() != 0 {
		t.Fatal("turn count should reset after ShouldReviewMemory returns true")
	}
}

func TestNudgeManagerShouldReviewSkillsResetsCounter(t *testing.T) {
	nm := setupTestNudgeManager(20, 2)
	nm.OnToolIteration("session1")
	nm.OnToolIteration("session1")
	nm.ShouldReviewSkills()
	if nm.GetToolIterCount() != 0 {
		t.Fatal("tool iter count should reset after ShouldReviewSkills returns true")
	}
}

func TestNudgeManagerBuildMemoryPrompt(t *testing.T) {
	nm := setupTestNudgeManager(3, 10)
	history := []providers.Message{
		{Role: "user", Content: "Tell me about kubernetes deployment strategies"},
		{Role: "assistant", Content: "Kubernetes supports several deployment strategies..."},
		{Role: "user", Content: "What about rolling updates vs blue-green?"},
	}
	prompt := nm.BuildMemoryPrompt(history)
	if prompt == "" {
		t.Fatal("expected non-empty memory prompt")
	}
	if !nudgeContains(prompt, "kubernetes") {
		t.Fatal("expected prompt to contain kubernetes")
	}
}

func TestNudgeManagerBuildMemoryPromptEmpty(t *testing.T) {
	nm := setupTestNudgeManager(3, 10)
	prompt := nm.BuildMemoryPrompt([]providers.Message{})
	if prompt != "" {
		t.Fatal("expected empty prompt for empty history")
	}
}

func TestNudgeManagerBuildSkillPrompt(t *testing.T) {
	nm := setupTestNudgeManager(20, 3)
	toolsUsed := []string{"bash", "bash", "bash", "read_file"}
	prompt := nm.BuildSkillPrompt(toolsUsed)
	if prompt == "" {
		t.Fatal("expected non-empty skill prompt")
	}
	if !nudgeContains(prompt, "bash") {
		t.Fatal("expected prompt to contain bash")
	}
}

func TestNudgeManagerBuildSkillPromptNoRepeat(t *testing.T) {
	nm := setupTestNudgeManager(20, 3)
	toolsUsed := []string{"bash", "read_file", "write_file"}
	prompt := nm.BuildSkillPrompt(toolsUsed)
	if prompt != "" {
		t.Fatal("expected empty prompt when no tool used 3+ times")
	}
}

func TestNudgeManagerBuildSkillPromptEmpty(t *testing.T) {
	nm := setupTestNudgeManager(20, 3)
	prompt := nm.BuildSkillPrompt([]string{})
	if prompt != "" {
		t.Fatal("expected empty prompt for empty tools list")
	}
}

func TestNudgeManagerZeroIntervalDisabled(t *testing.T) {
	nm := NewNudgeManager(NudgeConfig{Enabled: true, MemoryInterval: 0, SkillInterval: 0}, nil)
	nm.OnUserTurn("session1")
	nm.OnToolIteration("session1")
	if nm.ShouldReviewMemory() {
		t.Fatal("memory interval 0 should disable")
	}
	if nm.ShouldReviewSkills() {
		t.Fatal("skill interval 0 should disable")
	}
}

func nudgeContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && nudgeContainsSubstr(s, substr))
}

func nudgeContainsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
