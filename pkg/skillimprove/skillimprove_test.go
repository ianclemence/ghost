package skillimprove

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultImproveConfig(t *testing.T) {
	cfg := DefaultImproveConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.MinSuccessRatio != 0.6 {
		t.Errorf("default min success ratio should be 0.6, got %f", cfg.MinSuccessRatio)
	}
	if cfg.MinUsesBeforeImprove != 3 {
		t.Errorf("default min uses should be 3, got %d", cfg.MinUsesBeforeImprove)
	}
}

func TestSkillImprover_RecordUse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())

	si.RecordUse("weather", true, 150.0)
	si.RecordUse("weather", true, 200.0)
	si.RecordUse("weather", false, 100.0)

	perf := si.GetPerformance("weather")
	if perf == nil {
		t.Fatal("expected performance for weather")
	}
	if perf.TotalUses != 3 {
		t.Errorf("expected 3 total uses, got %d", perf.TotalUses)
	}
	if perf.SuccessCount != 2 {
		t.Errorf("expected 2 successes, got %d", perf.SuccessCount)
	}
	if perf.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", perf.FailureCount)
	}
	if perf.AvgDuration != 150.0 {
		t.Errorf("expected avg duration 150, got %f", perf.AvgDuration)
	}
}

func TestSkillImprover_RecordUse_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultImproveConfig()
	cfg.Enabled = false
	si := NewSkillImprover(tmpDir, cfg)

	si.RecordUse("weather", true, 100)

	if si.GetPerformance("weather") != nil {
		t.Error("expected no performance when disabled")
	}
}

func TestSkillImprover_ShouldImprove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultImproveConfig()
	cfg.MinUsesBeforeImprove = 2
	si := NewSkillImprover(tmpDir, cfg)

	// Not enough uses yet
	si.RecordUse("weather", true, 100)
	if si.ShouldImprove("weather") {
		t.Error("should not improve with only 1 use")
	}

	// Enough uses now
	si.RecordUse("weather", true, 100)
	if !si.ShouldImprove("weather") {
		t.Error("should improve with 2 uses and 100% success")
	}

	// Low success ratio
	si.RecordUse("weather", false, 100)
	si.RecordUse("weather", false, 100)
	if si.ShouldImprove("weather") {
		t.Error("should not improve with low success ratio")
	}
}

func TestSkillImprover_ShouldImprove_MaxImprovements(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultImproveConfig()
	cfg.MinUsesBeforeImprove = 1
	cfg.MaxImprovements = 2
	si := NewSkillImprover(tmpDir, cfg)

	si.RecordUse("weather", true, 100)

	// Add 2 pending improvements
	si.SuggestImprovement("weather", AddStep, "step 1", "ctx", 0.8)
	si.SuggestImprovement("weather", AddStep, "step 2", "ctx", 0.8)

	if si.ShouldImprove("weather") {
		t.Error("should not improve when max pending reached")
	}
}

func TestSkillImprover_SuggestImprovement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())

	si.SuggestImprovement("weather", AddStep, "Check wind speed", "wind data was missing", 0.9)

	imps := si.GetImprovements()
	if len(imps) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(imps))
	}
	if imps[0].Type != AddStep {
		t.Errorf("expected type add_step, got %s", imps[0].Type)
	}
	if imps[0].Status != "suggested" {
		t.Errorf("expected status suggested, got %s", imps[0].Status)
	}
}

func TestSkillImprover_ApplyImprovement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a skill directory
	skillDir := filepath.Join(tmpDir, "skills", "weather")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	skillContent := `---
name: weather
description: Check weather
---
# Weather Skill

## Steps

1. Check temperature
2. Check humidity
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())
	si.SuggestImprovement("weather", AddStep, "3. Check wind speed", "wind data was useful", 0.9)

	imps := si.GetPendingImprovements("weather")
	if len(imps) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(imps))
	}

	err = si.ApplyImprovement("weather", imps[0].ID)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// Read the updated skill
	updated, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !containsStr(string(updated), "Check wind speed") {
		t.Error("SKILL.md should contain the new step")
	}

	// Check improvement status
	imps2 := si.GetPendingImprovements("weather")
	if len(imps2) != 0 {
		t.Error("should have no pending improvements after apply")
	}
}

func TestSkillImprover_ApplyImprovement_RefineDescription(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillDir := filepath.Join(tmpDir, "skills", "weather")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	skillContent := `---
name: weather
description: Check weather
---
# Weather
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())
	si.SuggestImprovement("weather", RefineDesc, "Check current weather including wind and UV", "usage showed more features", 0.85)

	imps := si.GetPendingImprovements("weather")
	err = si.ApplyImprovement("weather", imps[0].ID)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	updated, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !containsStr(string(updated), "Check current weather including wind and UV") {
		t.Error("description should be updated")
	}
}

func TestSkillImprover_ApplyImprovement_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())

	err = si.ApplyImprovement("weather", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent improvement")
	}
}

func TestSkillImprover_RejectImprovement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())
	si.SuggestImprovement("weather", AddStep, "step", "ctx", 0.8)

	imps := si.GetPendingImprovements("weather")
	err = si.RejectImprovement("weather", imps[0].ID)
	if err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	if len(si.GetPendingImprovements("weather")) != 0 {
		t.Error("should have no pending after reject")
	}
}

func TestParseImprovementResponse(t *testing.T) {
	content := `{
		"improvements": [
			{"type": "add_step", "content": "3. Check wind", "confidence": 0.9},
			{"type": "add_notes", "content": "Works well for tropical regions", "confidence": 0.7}
		]
	}`

	imps := ParseImprovementResponse(content)
	if len(imps) != 2 {
		t.Fatalf("expected 2 improvements, got %d", len(imps))
	}
	if imps[0].Type != AddStep {
		t.Errorf("expected add_step, got %s", imps[0].Type)
	}
	if imps[1].Type != AddNotes {
		t.Errorf("expected add_notes, got %s", imps[1].Type)
	}
}

func TestParseImprovementResponse_MarkdownWrapped(t *testing.T) {
	content := "Here are improvements:\n```json\n{\"improvements\": [{\"type\": \"add_edge_case\", \"content\": \"handle rain\", \"confidence\": 0.8}]}\n```"

	imps := ParseImprovementResponse(content)
	if len(imps) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(imps))
	}
	if imps[0].Type != AddEdgeCase {
		t.Errorf("expected add_edge_case, got %s", imps[0].Type)
	}
}

func TestParseImprovementResponse_Invalid(t *testing.T) {
	imps := ParseImprovementResponse("no json here")
	if len(imps) != 0 {
		t.Errorf("expected 0 improvements for invalid input, got %d", len(imps))
	}
}

func TestSkillImprover_GetAllPerformance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())

	si.RecordUse("weather", true, 100)
	si.RecordUse("news", true, 200)
	si.RecordUse("news", true, 150)
	si.RecordUse("weather", true, 120)

	all := si.GetAllPerformance()
	if len(all) != 2 {
		t.Fatalf("expected 2 performance records, got %d", len(all))
	}

	// Should be sorted by total uses descending
	if all[0].TotalUses < all[1].TotalUses {
		t.Error("should be sorted by total uses descending")
	}
}

func TestSkillImprover_SaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())
	si.RecordUse("weather", true, 100)
	si.SuggestImprovement("weather", AddStep, "step", "ctx", 0.8)

	err = si.Save()
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load into new instance
	si2 := NewSkillImprover(tmpDir, DefaultImproveConfig())
	err = si2.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(si2.GetImprovements()) != 1 {
		t.Errorf("expected 1 improvement after load, got %d", len(si2.GetImprovements()))
	}

	perf := si2.GetPerformance("weather")
	if perf == nil || perf.TotalUses != 1 {
		t.Error("expected weather performance after load")
	}
}

func TestApplyImprovementToContent_AddEdgeCase(t *testing.T) {
	content := "# Skill\n\n## Steps\n\n1. Do something\n\n## Notes\n\nSome notes"
	imp := &Improvement{Type: AddEdgeCase, Content: "- Rainy days need umbrella"}

	result := applyImprovementToContent(content, imp)
	if !containsStr(result, "## Edge Cases") {
		t.Error("should create Edge Cases section")
	}
	if !containsStr(result, "Rainy days need umbrella") {
		t.Error("should contain edge case content")
	}
}

func TestApplyImprovementToContent_AddNotes(t *testing.T) {
	content := "# Skill\n\n## Steps\n\n1. Do something"
	imp := &Improvement{Type: AddNotes, Content: "- Works best in summer"}

	result := applyImprovementToContent(content, imp)
	if !containsStr(result, "## Notes") {
		t.Error("should create Notes section")
	}
	if !containsStr(result, "Works best in summer") {
		t.Error("should contain notes content")
	}
}

func TestApplyImprovementToContent_ReworkSteps(t *testing.T) {
	content := "# Skill\n\n## Steps\n\n1. Old step\n\n## Notes\n\nNotes"
	imp := &Improvement{Type: ReworkSteps, Content: "1. New step one\n2. New step two"}

	result := applyImprovementToContent(content, imp)
	if containsStr(result, "Old step") {
		t.Error("old step should be replaced")
	}
	if !containsStr(result, "New step one") {
		t.Error("should contain new steps")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

// Verify timing doesn't cause issues
func TestSkillImprover_RecordUse_Timing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skillimprove-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	si := NewSkillImprover(tmpDir, DefaultImproveConfig())

	start := time.Now()
	si.RecordUse("weather", true, 100)
	si.RecordUse("weather", true, 200)

	perf := si.GetPerformance("weather")
	if perf.LastUsed.Before(start) {
		t.Error("last used should be after start")
	}
}
