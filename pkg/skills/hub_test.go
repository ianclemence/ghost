package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSkillsHub_ListInstalled_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	hub := &SkillsHub{
		workspace:    tmpDir,
		syncFilePath: filepath.Join(tmpDir, ".skills-sync.json"),
		state:        HubSyncFile{Version: 1, Skills: []HubSyncState{}},
	}

	installed := hub.ListInstalled()
	if len(installed) != 0 {
		t.Errorf("expected 0 installed, got %d", len(installed))
	}
}

func TestSkillsHub_SetAutoUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	syncFile := filepath.Join(tmpDir, ".skills-sync.json")

	hub := &SkillsHub{
		workspace:    tmpDir,
		syncFilePath: syncFile,
		state: HubSyncFile{
			Version: 1,
			Skills: []HubSyncState{
				{Slug: "test-skill", Version: "1.0.0", AutoUpdate: true},
			},
		},
	}

	err := hub.SetAutoUpdate("test-skill", false)
	if err != nil {
		t.Fatalf("failed to set auto update: %v", err)
	}

	state, ok := hub.GetSyncState("test-skill")
	if !ok {
		t.Fatal("expected skill to be found")
	}
	if state.AutoUpdate {
		t.Error("expected auto update to be false")
	}
}

func TestSkillsHub_SetAutoUpdate_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	hub := &SkillsHub{
		workspace:    tmpDir,
		syncFilePath: filepath.Join(tmpDir, ".skills-sync.json"),
		state:        HubSyncFile{Version: 1, Skills: []HubSyncState{}},
	}

	err := hub.SetAutoUpdate("nonexistent", false)
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestSkillsHub_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	os.MkdirAll(skillDir, 0755)

	syncFile := filepath.Join(tmpDir, ".skills-sync.json")
	hub := &SkillsHub{
		workspace:    tmpDir,
		syncFilePath: syncFile,
		state: HubSyncFile{
			Version: 1,
			Skills: []HubSyncState{
				{Slug: "test-skill", Version: "1.0.0"},
			},
		},
	}

	err := hub.Remove("test-skill")
	if err != nil {
		t.Fatalf("failed to remove: %v", err)
	}

	// Check skill directory removed
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed")
	}

	// Check sync state removed
	if len(hub.state.Skills) != 0 {
		t.Errorf("expected 0 skills in sync state, got %d", len(hub.state.Skills))
	}
}

func TestSkillsHub_BuildHubSummary_Empty(t *testing.T) {
	hub := &SkillsHub{
		state: HubSyncFile{Skills: []HubSyncState{}},
	}

	summary := hub.BuildHubSummary()
	if summary != "No skills tracked by hub sync." {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestSkillsHub_BuildHubSummary_WithSkills(t *testing.T) {
	hub := &SkillsHub{
		state: HubSyncFile{
			Skills: []HubSyncState{
				{Slug: "skill-1", Version: "1.0.0", LastSynced: time.Now(), AutoUpdate: true},
				{Slug: "skill-2", Version: "2.0.0", LastSynced: time.Now(), AutoUpdate: false},
			},
		},
	}

	summary := hub.BuildHubSummary()
	if !containsStr(summary, "skill-1") {
		t.Errorf("expected skill-1 in summary: %s", summary)
	}
	if !containsStr(summary, "skill-2") {
		t.Errorf("expected skill-2 in summary: %s", summary)
	}
}

func TestSyncFileLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	syncFile := filepath.Join(tmpDir, ".skills-sync.json")

	// Save state
	state := HubSyncFile{
		Version: 1,
		Skills: []HubSyncState{
			{Slug: "test", Version: "1.0.0", AutoUpdate: true},
		},
	}
	saveSyncFile(syncFile, state)

	// Load state
	loaded := loadSyncFile(syncFile)
	if loaded.Version != 1 {
		t.Errorf("expected version 1, got %d", loaded.Version)
	}
	if len(loaded.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(loaded.Skills))
	}
	if loaded.Skills[0].Slug != "test" {
		t.Errorf("expected slug test, got %s", loaded.Skills[0].Slug)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
