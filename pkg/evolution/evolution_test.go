package evolution

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvolutionManager_RecordTurn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true

	em := NewEvolutionManager(tmpDir, config)

	em.RecordTurn(LearningRecord{
		TaskKind:   "web_search",
		ToolsUsed:  []string{"web_search", "web_fetch"},
		SkillsUsed: []string{"research"},
		Success:    true,
		Duration:   5 * time.Second,
		SessionKey: "test-session",
	})

	records := em.GetRecords()
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].TaskKind != "web_search" {
		t.Errorf("Expected task kind 'web_search', got '%s'", records[0].TaskKind)
	}
}

func TestEvolutionManager_DisabledNoOp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = false

	em := NewEvolutionManager(tmpDir, config)

	em.RecordTurn(LearningRecord{
		TaskKind: "test",
		Success:  true,
	})

	if len(em.GetRecords()) != 0 {
		t.Error("Expected no records when disabled")
	}
}

func TestEvolutionManager_ClusterPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true
	config.MinSuccessRatio = 0.5

	em := NewEvolutionManager(tmpDir, config)

	// Record 3 successful web_search tasks and 1 failure
	for i := 0; i < 3; i++ {
		em.RecordTurn(LearningRecord{
			TaskKind:  "web_search",
			ToolsUsed: []string{"web_search"},
			Success:   true,
		})
	}
	em.RecordTurn(LearningRecord{
		TaskKind:  "web_search",
		ToolsUsed: []string{"web_search"},
		Success:   false,
	})

	err = em.RunColdPath()
	if err != nil {
		t.Fatalf("Cold path failed: %v", err)
	}

	patterns := em.GetPatterns()
	if len(patterns) != 1 {
		t.Fatalf("Expected 1 pattern, got %d", len(patterns))
	}

	if patterns[0].TaskKind != "web_search" {
		t.Errorf("Expected pattern task kind 'web_search', got '%s'", patterns[0].TaskKind)
	}

	if patterns[0].Count != 4 {
		t.Errorf("Expected pattern count 4, got %d", patterns[0].Count)
	}
}

func TestEvolutionManager_GenerateDrafts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true
	config.MinSuccessRatio = 0.5

	em := NewEvolutionManager(tmpDir, config)

	// Record 4 successful tasks of same kind (need >= 3 for draft generation)
	for i := 0; i < 4; i++ {
		em.RecordTurn(LearningRecord{
			TaskKind:  "file_edit",
			ToolsUsed: []string{"read_file", "edit_file"},
			Success:   true,
		})
	}

	em.RunColdPath()

	drafts := em.GetDrafts()
	if len(drafts) != 1 {
		t.Fatalf("Expected 1 draft, got %d", len(drafts))
	}

	if drafts[0].SkillName != "auto_file_edit" {
		t.Errorf("Expected skill name 'auto_file_edit', got '%s'", drafts[0].SkillName)
	}

	if drafts[0].Status != "candidate" {
		t.Errorf("Expected status 'candidate', got '%s'", drafts[0].Status)
	}
}

func TestEvolutionManager_LifecycleTransitions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true
	config.ColdAfterDays = 0 // Immediately cold for testing

	em := NewEvolutionManager(tmpDir, config)

	// Add a profile manually
	em.mu.Lock()
	em.profiles["old_skill"] = &SkillProfile{
		Name:           "old_skill",
		Status:         SkillStatusActive,
		UsageCount:     1,
		RetentionScore: 0.01, // Low score
		LastUsed:       time.Now().Add(-24 * time.Hour * 100), // 100 days ago
		CreatedAt:      time.Now().Add(-24 * time.Hour * 200),
		UpdatedAt:      time.Now().Add(-24 * time.Hour * 100),
	}
	em.mu.Unlock()

	em.RunColdPath()

	profiles := em.GetProfiles()
	for _, p := range profiles {
		if p.Name == "old_skill" {
			if p.Status != SkillStatusCold {
				t.Errorf("Expected 'old_skill' to be cold, got '%s'", p.Status)
			}
		}
	}
}

func TestEvolutionManager_ApplyDraft(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true

	em := NewEvolutionManager(tmpDir, config)

	// Add a draft manually
	em.mu.Lock()
	em.drafts = append(em.drafts, SkillDraft{
		ID:          "test-draft",
		SkillName:   "test_skill",
		Description: "Test skill",
		ChangeKind:  "create",
		Body:        "---\nname: test_skill\ndescription: Test\n---\n\n# Test\n\nStep 1",
		Status:      "candidate",
		CreatedAt:   time.Now(),
	})
	em.mu.Unlock()

	err = em.ApplyDraft("test-draft")
	if err != nil {
		t.Fatalf("ApplyDraft failed: %v", err)
	}

	// Verify file was created
	skillPath := filepath.Join(tmpDir, "skills", "test_skill", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Error("Expected skill file to be created")
	}

	// Verify draft status changed
	drafts := em.GetDrafts()
	for _, d := range drafts {
		if d.ID == "test-draft" {
			if d.Status != "applied" {
				t.Errorf("Expected status 'applied', got '%s'", d.Status)
			}
		}
	}
}

func TestEvolutionManager_ApplyDraft_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	em := NewEvolutionManager(tmpDir, DefaultEvolutionConfig())

	err = em.ApplyDraft("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent draft")
	}
}

func TestEvolutionManager_SaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultEvolutionConfig()
	config.Enabled = true

	em := NewEvolutionManager(tmpDir, config)

	// Add some data
	em.RecordTurn(LearningRecord{
		TaskKind: "test",
		Success:  true,
	})

	em.mu.Lock()
	em.patterns["test"] = &Pattern{
		ID:       "pat-1",
		TaskKind: "test",
		Count:    5,
	}
	em.mu.Unlock()

	// Save
	if err := em.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new manager
	em2 := NewEvolutionManager(tmpDir, config)
	if err := em2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	records := em2.GetRecords()
	if len(records) != 1 {
		t.Errorf("Expected 1 record after load, got %d", len(records))
	}

	patterns := em2.GetPatterns()
	if len(patterns) != 1 {
		t.Errorf("Expected 1 pattern after load, got %d", len(patterns))
	}
}

func TestEvolutionManager_GetProfiles_Sorted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evolution-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	em := NewEvolutionManager(tmpDir, DefaultEvolutionConfig())

	em.mu.Lock()
	em.profiles["zebra"] = &SkillProfile{Name: "zebra"}
	em.profiles["alpha"] = &SkillProfile{Name: "alpha"}
	em.profiles["middle"] = &SkillProfile{Name: "middle"}
	em.mu.Unlock()

	profiles := em.GetProfiles()
	if len(profiles) != 3 {
		t.Fatalf("Expected 3 profiles, got %d", len(profiles))
	}

	// Should be sorted by name
	if profiles[0].Name != "alpha" || profiles[1].Name != "middle" || profiles[2].Name != "zebra" {
		t.Errorf("Expected sorted profiles, got %v", profiles)
	}
}

func TestDefaultEvolutionConfig(t *testing.T) {
	config := DefaultEvolutionConfig()

	if config.Enabled {
		t.Error("Expected disabled by default")
	}
	if config.ColdAfterDays != 90 {
		t.Errorf("Expected 90 cold days, got %d", config.ColdAfterDays)
	}
}
