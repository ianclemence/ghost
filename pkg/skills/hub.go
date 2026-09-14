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
	Slug       string    `json:"slug"`
	Version    string    `json:"version"`
	LastSynced time.Time `json:"last_synced"`
	LocalPath  string    `json:"local_path"`
	Source     string    `json:"source"` // "clawhub", "github", "local"
	AutoUpdate bool      `json:"auto_update"`
	// Requires records the Ghost capabilities the installed version
	// declares (`requires:` frontmatter). A request, never a grant — but
	// recorded so readiness and evals can check it.
	Requires []string `json:"requires,omitempty"`
	// PreviousVersion is the last working version kept for rollback.
	// Empty when no rollback snapshot exists.
	PreviousVersion string `json:"previous_version,omitempty"`
}

// HubSyncFile is the on-disk format for sync state.
type HubSyncFile struct {
	Version int            `json:"version"`
	Skills  []HubSyncState `json:"skills"`
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

	// Capability-scoped install: the skill's declared `requires:` must name
	// capabilities Ghost knows. An unknown name is a broken skill (typo or
	// drift), so fail closed before recording sync state. This validates
	// the request; granting still happens only through the broker at runtime.
	requires := h.installedRequires(filepath.Join(targetDir, slug))
	for _, req := range requires {
		if !HasCapability(req) && !isKnownGhostCapability(req) {
			return nil, fmt.Errorf("skill %q requires unknown capability %q", slug, req)
		}
	}

	// Record sync state (dedupe: reinstalls update the existing row).
	found := false
	for i, s := range h.state.Skills {
		if s.Slug == slug {
			h.state.Skills[i].Version = result.Version
			h.state.Skills[i].LastSynced = time.Now()
			h.state.Skills[i].Requires = requires
			found = true
			break
		}
	}
	if !found {
		h.state.Skills = append(h.state.Skills, HubSyncState{
			Slug:       slug,
			Version:    result.Version,
			LastSynced: time.Now(),
			LocalPath:  filepath.Join(targetDir, slug),
			Source:     "clawhub",
			AutoUpdate: true,
			Requires:   requires,
		})
	}
	saveSyncFile(h.syncFilePath, h.state)

	logger.InfoCF("skills-hub", "Skill installed from hub", map[string]interface{}{
		"slug":    slug,
		"version": result.Version,
	})

	return result, nil
}

// installedRequires reads the installed skill's declared capabilities.
func (h *SkillsHub) installedRequires(skillDir string) []string {
	if h.loader == nil {
		return nil
	}
	if meta := h.loader.getSkillMetadata(filepath.Join(skillDir, "SKILL.md")); meta != nil {
		return meta.RequiresCapabilities
	}
	return nil
}

// isKnownGhostCapability reports whether id is a runtime capability outside
// the skill-codec registry (model-facing capabilities resolved by the
// capability resolver, e.g. memory.recall, web.search, email.read).
func isKnownGhostCapability(id string) bool {
	switch id {
	case "memory.recall", "memory.remember",
		"web.search", "web.fetch",
		"file.read", "file.write",
		"artifact.create",
		"weather.get", "aqi.get", "currency.convert", "crypto.price",
		"places.nearby", "flight.status",
		"email.read", "email.send",
		"media.playback", "code.read", "repository.search", "docs",
		"calendar.read", "calendar.modify",
		"browser.inspect", "browser.control", "browser.transact",
		"computer.inspect", "computer.control",
		"device.read", "device.control",
		"message.send",
		"routine.create", "routine.modify", "routine.cancel",
		"goal.manage":
		return true
	default:
		return false
	}
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

	// Snapshot the working version for rollback before touching it.
	oldPath := h.state.Skills[idx].LocalPath
	oldVersion := h.state.Skills[idx].Version
	backupDir, err := snapshotSkillDir(oldPath)
	if err != nil {
		return nil, fmt.Errorf("cannot snapshot %q for rollback: %w", slug, err)
	}

	// Remove old version and install new.
	os.RemoveAll(oldPath)

	result, err := h.registry.DownloadAndInstall(ctx, slug, meta.LatestVersion, filepath.Join(h.workspace, "skills"))
	if err != nil {
		// Roll back: restore the working version, keep sync state intact.
		_ = os.RemoveAll(oldPath)
		if rerr := restoreSkillDir(backupDir, oldPath); rerr != nil {
			return nil, fmt.Errorf("update to %s failed (%v) and rollback failed (%v)", meta.LatestVersion, err, rerr)
		}
		return nil, fmt.Errorf("update to %s failed, rolled back to %s: %w", meta.LatestVersion, oldVersion, err)
	}

	// Validate the new version's declared capabilities before recording.
	// Unknown names mean a broken skill: roll back to the working version.
	requires := h.installedRequires(filepath.Join(h.workspace, "skills", slug))
	for _, req := range requires {
		if !HasCapability(req) && !isKnownGhostCapability(req) {
			_ = os.RemoveAll(filepath.Join(h.workspace, "skills", slug))
			_ = restoreSkillDir(backupDir, oldPath)
			return nil, fmt.Errorf("skill %q v%s requires unknown capability %q, kept %s", slug, result.Version, req, oldVersion)
		}
	}
	// Static evals gate the new version: a version that fails eval never
	// replaces the working one (roll back, keep sync state intact).
	if evals := EvalSkill(filepath.Join(h.workspace, "skills", slug)); !EvalPassed(evals) {
		_ = os.RemoveAll(filepath.Join(h.workspace, "skills", slug))
		_ = restoreSkillDir(backupDir, oldPath)
		return nil, fmt.Errorf("skill %q v%s failed eval (%s), kept %s", slug, result.Version, evalFailure(evals), oldVersion)
	}
	_ = os.RemoveAll(backupDir)

	h.state.Skills[idx].Version = result.Version
	h.state.Skills[idx].PreviousVersion = oldVersion
	h.state.Skills[idx].LastSynced = time.Now()
	h.state.Skills[idx].Requires = requires
	saveSyncFile(h.syncFilePath, h.state)

	logger.InfoCF("skills-hub", "Skill updated", map[string]interface{}{
		"slug":        slug,
		"old_version": oldVersion,
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

// snapshotSkillDir copies a skill directory to a temp backup for rollback.
// Returns the backup path; absent source yields an empty path and nil error
// (fresh install, nothing to roll back to).
func snapshotSkillDir(src string) (string, error) {
	fi, err := os.Stat(src)
	if err != nil || !fi.IsDir() {
		return "", nil
	}
	dst, err := os.MkdirTemp("", "ghost-skill-rollback-")
	if err != nil {
		return "", err
	}
	_ = os.Remove(dst)
	if err := copyDirRecursive(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}

// restoreSkillDir moves a snapshot back over dst.
func restoreSkillDir(backup, dst string) error {
	if backup == "" {
		return fmt.Errorf("no rollback snapshot")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := copyDirRecursive(backup, dst); err != nil {
		return err
	}
	return os.RemoveAll(backup)
}

func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
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
