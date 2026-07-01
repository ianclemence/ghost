package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// SkillStatus represents the lifecycle status of a skill.
type SkillStatus string

const (
	SkillStatusActive   SkillStatus = "active"
	SkillStatusCold     SkillStatus = "cold"
	SkillStatusArchived SkillStatus = "archived"
	SkillStatusDeleted  SkillStatus = "deleted"
)

// LearningRecord captures data from a completed agent turn.
type LearningRecord struct {
	ID           string            `json:"id"`
	TaskKind     string            `json:"task_kind"`
	Summary      string            `json:"summary"`
	ToolsUsed    []string          `json:"tools_used"`
	SkillsUsed   []string          `json:"skills_used"`
	Success      bool              `json:"success"`
	Duration     time.Duration     `json:"duration"`
	SessionKey   string            `json:"session_key"`
	Workspace    string            `json:"workspace"`
	Timestamp    time.Time         `json:"timestamp"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// SkillDraft represents a proposed skill modification.
type SkillDraft struct {
	ID          string     `json:"id"`
	PatternID   string     `json:"pattern_id"`
	SkillName   string     `json:"skill_name"`
	Description string     `json:"description"`
	ChangeKind  string     `json:"change_kind"` // "create", "patch", "replace"
	Body        string     `json:"body"`
	Status      string     `json:"status"` // "candidate", "quarantined", "applied"
	CreatedAt   time.Time  `json:"created_at"`
}

// SkillProfile tracks a skill's usage and lifecycle.
type SkillProfile struct {
	Name          string        `json:"name"`
	Status        SkillStatus   `json:"status"`
	UsageCount    int           `json:"usage_count"`
	RetentionScore float64      `json:"retention_score"`
	Version       int           `json:"version"`
	LastUsed      time.Time     `json:"last_used"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Pattern represents a cluster of similar successful tasks.
type Pattern struct {
	ID          string    `json:"id"`
	TaskKind    string    `json:"task_kind"`
	Count       int       `json:"count"`
	WinningPath []string  `json:"winning_path"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// EvolutionConfig configures the evolution pipeline.
type EvolutionConfig struct {
	Enabled           bool          `json:"enabled"`
	MinSuccessRatio   float64       `json:"min_success_ratio"`
	ColdAfterDays     int           `json:"cold_after_days"`
	ArchiveAfterDays  int           `json:"archive_after_days"`
	DeleteAfterDays   int           `json:"delete_after_days"`
	MinRetentionScore float64       `json:"min_retention_score"`
}

// DefaultEvolutionConfig returns sensible defaults.
func DefaultEvolutionConfig() EvolutionConfig {
	return EvolutionConfig{
		Enabled:           false,
		MinSuccessRatio:   0.6,
		ColdAfterDays:     90,
		ArchiveAfterDays:  180,
		DeleteAfterDays:   365,
		MinRetentionScore: 0.3,
	}
}

// EvolutionManager manages the self-evolution pipeline.
type EvolutionManager struct {
	config    EvolutionConfig
	workspace string
	records   []LearningRecord
	patterns  map[string]*Pattern
	drafts    []SkillDraft
	profiles  map[string]*SkillProfile
	mu        sync.RWMutex
}

// NewEvolutionManager creates a new EvolutionManager.
func NewEvolutionManager(workspace string, config EvolutionConfig) *EvolutionManager {
	return &EvolutionManager{
		config:    config,
		workspace: workspace,
		records:   make([]LearningRecord, 0),
		patterns:  make(map[string]*Pattern),
		drafts:    make([]SkillDraft, 0),
		profiles:  make(map[string]*SkillProfile),
	}
}

// RecordTurn records the outcome of an agent turn (hot path).
func (em *EvolutionManager) RecordTurn(record LearningRecord) {
	if !em.config.Enabled {
		return
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	if record.ID == "" {
		record.ID = fmt.Sprintf("rec_%d", time.Now().UnixNano())
	}

	em.records = append(em.records, record)

	logger.DebugCF("evolution", "Recorded turn", map[string]interface{}{
		"id":       record.ID,
		"success":  record.Success,
		"task":     record.TaskKind,
	})
}

// GetRecords returns a copy of all learning records.
func (em *EvolutionManager) GetRecords() []LearningRecord {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make([]LearningRecord, len(em.records))
	copy(result, em.records)
	return result
}

// RunColdPath executes the cold path analysis (pattern clustering, draft generation).
// This should be called periodically (e.g., via cron).
func (em *EvolutionManager) RunColdPath() error {
	if !em.config.Enabled {
		return nil
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	// 1. Cluster successful tasks into patterns
	em.clusterPatterns()

	// 2. Generate drafts for ready patterns
	em.generateDrafts()

	// 3. Update skill profiles
	em.updateProfiles()

	// 4. Run lifecycle maintenance
	em.runLifecycle()

	logger.InfoCF("evolution", "Cold path completed", map[string]interface{}{
		"patterns": len(em.patterns),
		"drafts":   len(em.drafts),
		"profiles": len(em.profiles),
	})

	return nil
}

// clusterPatterns groups similar successful tasks into patterns.
func (em *EvolutionManager) clusterPatterns() {
	// Count success by task kind
	successCounts := make(map[string]int)
	totalCounts := make(map[string]int)

	for _, rec := range em.records {
		totalCounts[rec.TaskKind]++
		if rec.Success {
			successCounts[rec.TaskKind]++
		}
	}

	// Create or update patterns for task kinds that meet the success threshold
	for kind, total := range totalCounts {
		success := successCounts[kind]
		ratio := float64(success) / float64(total)

		if ratio < em.config.MinSuccessRatio {
			continue
		}

		if total < 2 {
			continue // Need at least 2 occurrences
		}

		if existing, ok := em.patterns[kind]; ok {
			existing.Count = total
			existing.LastSeen = time.Now()
		} else {
			em.patterns[kind] = &Pattern{
				ID:          fmt.Sprintf("pat_%s_%d", kind, time.Now().UnixMilli()),
				TaskKind:    kind,
				Count:       total,
				WinningPath: em.findWinningPath(kind),
				FirstSeen:   time.Now(),
				LastSeen:    time.Now(),
			}
		}
	}
}

// findWinningPath finds the most common tool sequence for a task kind.
func (em *EvolutionManager) findWinningPath(taskKind string) []string {
	pathCounts := make(map[string]int)

	for _, rec := range em.records {
		if rec.TaskKind != taskKind || !rec.Success {
			continue
		}
		key := strings.Join(rec.ToolsUsed, ",")
		pathCounts[key]++
	}

	var bestPath string
	var bestCount int
	for path, count := range pathCounts {
		if count > bestCount {
			bestPath = path
			bestCount = count
		}
	}

	if bestPath == "" {
		return nil
	}
	return strings.Split(bestPath, ",")
}

// generateDrafts creates skill drafts for patterns that are ready.
func (em *EvolutionManager) generateDrafts() {
	for _, pattern := range em.patterns {
		// Only generate drafts for patterns with enough data
		if pattern.Count < 3 {
			continue
		}

		// Check if a draft already exists for this pattern
		if em.hasDraftForPattern(pattern.ID) {
			continue
		}

		draft := SkillDraft{
			ID:          fmt.Sprintf("draft_%d", time.Now().UnixNano()),
			PatternID:   pattern.ID,
			SkillName:   fmt.Sprintf("auto_%s", pattern.TaskKind),
			Description: fmt.Sprintf("Auto-generated skill for task kind: %s", pattern.TaskKind),
			ChangeKind:  "create",
			Body:        em.generateSkillBody(pattern),
			Status:      "candidate",
			CreatedAt:   time.Now(),
		}

		em.drafts = append(em.drafts, draft)

		logger.InfoCF("evolution", "Generated skill draft", map[string]interface{}{
			"pattern": pattern.ID,
			"draft":   draft.ID,
			"skill":   draft.SkillName,
		})
	}
}

// generateSkillBody creates a SKILL.md body from a pattern.
func (em *EvolutionManager) generateSkillBody(pattern *Pattern) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n",
		pattern.TaskKind, fmt.Sprintf("Auto-generated for %s tasks", pattern.TaskKind)))
	sb.WriteString(fmt.Sprintf("# %s\n\n", strings.Title(pattern.TaskKind)))
	sb.WriteString("## Steps\n\n")
	for i, tool := range pattern.WinningPath {
		sb.WriteString(fmt.Sprintf("%d. Use `%s`\n", i+1, tool))
	}
	sb.WriteString("\n## Notes\n\n")
	sb.WriteString(fmt.Sprintf("This skill was auto-generated from %d successful task observations.\n", pattern.Count))
	return sb.String()
}

// hasDraftForPattern checks if a draft already exists for a pattern.
func (em *EvolutionManager) hasDraftForPattern(patternID string) bool {
	for _, d := range em.drafts {
		if d.PatternID == patternID && d.Status == "candidate" {
			return true
		}
	}
	return false
}

// updateProfiles updates skill usage profiles.
func (em *EvolutionManager) updateProfiles() {
	// Count usage from records
	usageCounts := make(map[string]int)
	for _, rec := range em.records {
		for _, skill := range rec.SkillsUsed {
			usageCounts[skill]++
		}
	}

	now := time.Now()
	for name, count := range usageCounts {
		if profile, ok := em.profiles[name]; ok {
			profile.UsageCount = count
			profile.LastUsed = now
			profile.UpdatedAt = now
			profile.RetentionScore = float64(count) / float64(len(em.records))
		} else {
			em.profiles[name] = &SkillProfile{
				Name:           name,
				Status:         SkillStatusActive,
				UsageCount:     count,
				RetentionScore: float64(count) / float64(len(em.records)),
				Version:        1,
				LastUsed:       now,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
		}
	}
}

// runLifecycle transitions skills through their lifecycle.
func (em *EvolutionManager) runLifecycle() {
	now := time.Now()

	for _, profile := range em.profiles {
		if profile.Status == SkillStatusDeleted {
			continue
		}

		idleDays := int(now.Sub(profile.LastUsed).Hours() / 24)

		switch profile.Status {
		case SkillStatusActive:
			if idleDays > em.config.ColdAfterDays && profile.RetentionScore < em.config.MinRetentionScore {
				profile.Status = SkillStatusCold
				profile.UpdatedAt = now
				logger.InfoCF("evolution", "Skill transitioned to cold", map[string]interface{}{
					"skill":  profile.Name,
					"idle":   idleDays,
					"score":  profile.RetentionScore,
				})
			}
		case SkillStatusCold:
			if idleDays > em.config.ArchiveAfterDays && profile.RetentionScore < em.config.MinRetentionScore*0.7 {
				profile.Status = SkillStatusArchived
				profile.UpdatedAt = now
			}
		case SkillStatusArchived:
			if idleDays > em.config.DeleteAfterDays && profile.RetentionScore < em.config.MinRetentionScore*0.3 {
				profile.Status = SkillStatusDeleted
				profile.UpdatedAt = now
			}
		}
	}
}

// GetProfiles returns all skill profiles.
func (em *EvolutionManager) GetProfiles() []*SkillProfile {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make([]*SkillProfile, 0, len(em.profiles))
	for _, p := range em.profiles {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// GetDrafts returns all skill drafts.
func (em *EvolutionManager) GetDrafts() []SkillDraft {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make([]SkillDraft, len(em.drafts))
	copy(result, em.drafts)
	return result
}

// GetPatterns returns all detected patterns.
func (em *EvolutionManager) GetPatterns() []*Pattern {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make([]*Pattern, 0, len(em.patterns))
	for _, p := range em.patterns {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result
}

// ApplyDraft applies a skill draft to the workspace.
func (em *EvolutionManager) ApplyDraft(draftID string) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	var draft *SkillDraft
	for i := range em.drafts {
		if em.drafts[i].ID == draftID {
			draft = &em.drafts[i]
			break
		}
	}

	if draft == nil {
		return fmt.Errorf("draft %s not found", draftID)
	}

	if draft.Status != "candidate" {
		return fmt.Errorf("draft %s is not a candidate (status: %s)", draftID, draft.Status)
	}

	// Create skill directory
	skillDir := filepath.Join(em.workspace, "skills", draft.SkillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	// Write SKILL.md
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(draft.Body), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	draft.Status = "applied"

	logger.InfoCF("evolution", "Skill draft applied", map[string]interface{}{
		"draft":  draftID,
		"skill":  draft.SkillName,
	})

	return nil
}

// Save persists the evolution state to disk.
func (em *EvolutionManager) Save() error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	stateDir := filepath.Join(em.workspace, "state", "evolution")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	// Save records
	data, err := json.MarshalIndent(em.records, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "records.json"), data, 0644); err != nil {
		return err
	}

	// Save patterns
	data, err = json.MarshalIndent(em.patterns, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "patterns.json"), data, 0644); err != nil {
		return err
	}

	// Save drafts
	data, err = json.MarshalIndent(em.drafts, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "drafts.json"), data, 0644); err != nil {
		return err
	}

	// Save profiles
	data, err = json.MarshalIndent(em.profiles, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "profiles.json"), data, 0644); err != nil {
		return err
	}

	return nil
}

// Load restores the evolution state from disk.
func (em *EvolutionManager) Load() error {
	em.mu.Lock()
	defer em.mu.Unlock()

	stateDir := filepath.Join(em.workspace, "state", "evolution")

	// Load records
	if data, err := os.ReadFile(filepath.Join(stateDir, "records.json")); err == nil {
		var records []LearningRecord
		if json.Unmarshal(data, &records) == nil {
			em.records = records
		}
	}

	// Load patterns
	if data, err := os.ReadFile(filepath.Join(stateDir, "patterns.json")); err == nil {
		var patterns map[string]*Pattern
		if json.Unmarshal(data, &patterns) == nil {
			em.patterns = patterns
		}
	}

	// Load drafts
	if data, err := os.ReadFile(filepath.Join(stateDir, "drafts.json")); err == nil {
		var drafts []SkillDraft
		if json.Unmarshal(data, &drafts) == nil {
			em.drafts = drafts
		}
	}

	// Load profiles
	if data, err := os.ReadFile(filepath.Join(stateDir, "profiles.json")); err == nil {
		var profiles map[string]*SkillProfile
		if json.Unmarshal(data, &profiles) == nil {
			em.profiles = profiles
		}
	}

	return nil
}
