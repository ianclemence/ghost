package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

// HubSyncState tracks the sync state of each skill with the hub.
type HubSyncState struct {
	Slug         string    `json:"slug"`
	Version      string    `json:"version"`
	LastSynced   time.Time `json:"last_synced"`
	LocalPath    string    `json:"local_path"`
	Source       string    `json:"source"` // "clawhub", "github", "local"
	AutoUpdate   bool      `json:"auto_update"`
}

// HubSyncFile is the on-disk format for sync state.
type HubSyncFile struct {
	Version int              `json:"version"`
	Skills  []HubSyncState   `json:"skills"`
}

// SkillsHub manages bidirectional synchronization with the ClawHub registry.
type SkillsHub struct {
	registry     *ClawHubRegistry
	loader       *SkillsLoader
	workspace    string
	syncFilePath string
	state        HubSyncFile
}

// NewSkillsHub creates a new SkillsHub instance.
func NewSkillsHub(cfg *config.Config, workspace string) *SkillsHub {
	registry := NewClawHubRegistry(cfg.Skills.ClawHub)
	loader := NewSkillsLoader(workspace,
		filepath.Join(os.Getenv("HOME"), ".GHOST", "skills"),
		filepath.Join(os.Getenv("HOME"), ".GHOST", "ghost", "skills"))

	syncFilePath := filepath.Join(workspace, ".skills-sync.json")
	state := loadSyncFile(syncFilePath)

	return &SkillsHub{
		registry:     registry,
		loader:       loader,
		workspace:    workspace,
		syncFilePath: syncFilePath,
		state:        state,
	}
}

// Search searches the ClawHub registry for skills.
func (h *SkillsHub) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return h.registry.Search(ctx, query, limit)
}

// Install installs a skill from ClawHub and records sync state.
func (h *SkillsHub) Install(ctx context.Context, slug, version string) (*InstallResult, error) {
	targetDir := filepath.Join(h.workspace, "skills")
	result, err := h.registry.DownloadAndInstall(ctx, slug, version, targetDir)
	if err != nil {
		return nil, err
	}

	// Record sync state
	h.state.Skills = append(h.state.Skills, HubSyncState{
		Slug:       slug,
		Version:    result.Version,
		LastSynced: time.Now(),
		LocalPath:  filepath.Join(targetDir, slug),
		Source:     "clawhub",
		AutoUpdate: true,
	})
	saveSyncFile(h.syncFilePath, h.state)

	logger.InfoCF("skills-hub", "Skill installed from hub", map[string]interface{}{
		"slug":    slug,
		"version": result.Version,
	})

	return result, nil
}

// Update checks for updates and installs newer versions of installed skills.
func (h *SkillsHub) Update(ctx context.Context, slug string) (*InstallResult, error) {
	// Find existing sync state
	idx := -1
	for i, s := range h.state.Skills {
		if s.Slug == slug {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("skill %q is not tracked by hub sync", slug)
	}

	// Check latest version
	meta, err := h.registry.GetSkillMeta(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}

	if meta.LatestVersion == h.state.Skills[idx].Version {
		return nil, fmt.Errorf("skill %q is already at latest version %s", slug, meta.LatestVersion)
	}

	// Remove old version and install new
	oldPath := h.state.Skills[idx].LocalPath
	os.RemoveAll(oldPath)

	result, err := h.Install(ctx, slug, meta.LatestVersion)
	if err != nil {
		return nil, err
	}

	logger.InfoCF("skills-hub", "Skill updated", map[string]interface{}{
		"slug":     slug,
		"old_version": h.state.Skills[idx].Version,
		"new_version": result.Version,
	})

	return result, nil
}

// UpdateAll updates all tracked skills that have auto_update enabled.
func (h *SkillsHub) UpdateAll(ctx context.Context) []HubUpdateResult {
	var results []HubUpdateResult
	for _, s := range h.state.Skills {
		if !s.AutoUpdate {
			continue
		}
		result, err := h.Update(ctx, s.Slug)
		results = append(results, HubUpdateResult{
			Slug:    s.Slug,
			Success: err == nil,
			Result:  result,
			Error:   err,
		})
	}
	return results
}

// Remove removes a skill and its sync tracking.
func (h *SkillsHub) Remove(slug string) error {
	// Find and remove from sync state
	for i, s := range h.state.Skills {
		if s.Slug == slug {
			h.state.Skills = append(h.state.Skills[:i], h.state.Skills[i+1:]...)
			break
		}
	}
	saveSyncFile(h.syncFilePath, h.state)

	// Remove the skill directory
	skillDir := filepath.Join(h.workspace, "skills", slug)
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill directory: %w", err)
	}

	return nil
}

// ListInstalled returns all skills tracked by hub sync.
func (h *SkillsHub) ListInstalled() []HubSyncState {
	return h.state.Skills
}

// GetSyncState returns the sync state for a specific skill.
func (h *SkillsHub) GetSyncState(slug string) (*HubSyncState, bool) {
	for i, s := range h.state.Skills {
		if s.Slug == slug {
			return &h.state.Skills[i], true
		}
	}
	return nil, false
}

// SetAutoUpdate enables or disables auto-update for a skill.
func (h *SkillsHub) SetAutoUpdate(slug string, enabled bool) error {
	for i, s := range h.state.Skills {
		if s.Slug == slug {
			h.state.Skills[i].AutoUpdate = enabled
			saveSyncFile(h.syncFilePath, h.state)
			return nil
		}
	}
	return fmt.Errorf("skill %q not found in hub sync", slug)
}

// HubUpdateResult represents the result of updating a single skill.
type HubUpdateResult struct {
	Slug    string
	Success bool
	Result  *InstallResult
	Error   error
}

func loadSyncFile(path string) HubSyncFile {
	var state HubSyncFile
	data, err := os.ReadFile(path)
	if err != nil {
		return HubSyncFile{Version: 1, Skills: []HubSyncState{}}
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return HubSyncFile{Version: 1, Skills: []HubSyncState{}}
	}
	if state.Skills == nil {
		state.Skills = []HubSyncState{}
	}
	return state
}

func saveSyncFile(path string, state HubSyncFile) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)
	os.WriteFile(path, data, 0644)
}

// BuildHubSummary returns a human-readable summary of hub-tracked skills.
func (h *SkillsHub) BuildHubSummary() string {
	if len(h.state.Skills) == 0 {
		return "No skills tracked by hub sync."
	}

	var lines []string
	lines = append(lines, "Hub-tracked skills:")
	for _, s := range h.state.Skills {
		autoStr := ""
		if s.AutoUpdate {
			autoStr = " [auto-update]"
		}
		lines = append(lines, fmt.Sprintf("  - %s v%s (synced %s)%s",
			s.Slug, s.Version,
			s.LastSynced.Format("2006-01-02 15:04"),
			autoStr))
	}
	return strings.Join(lines, "\n")
}
