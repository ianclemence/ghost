// Package skillimprove implements skills self-improvement.
// After a skill is used successfully, the agent can analyze the interaction
// and suggest improvements to the SKILL.md file. The system tracks skill
// performance over time and generates targeted improvement suggestions.
//
// Improvement types:
// - add_step: append a new step discovered during use
// - refine_description: update the skill description based on actual usage
// - add_edge_case: document an edge case encountered
// - add_notes: add notes about what worked well
package skillimprove

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

// ImprovementType categorizes what kind of improvement is suggested.
type ImprovementType string

const (
	AddStep          ImprovementType = "add_step"
	RefineDesc       ImprovementType = "refine_description"
	AddEdgeCase      ImprovementType = "add_edge_case"
	AddNotes         ImprovementType = "add_notes"
	ReworkSteps      ImprovementType = "rework_steps"
)

// Improvement represents a suggested change to a skill.
type Improvement struct {
	ID          string           `json:"id"`
	SkillName   string           `json:"skill_name"`
	Type        ImprovementType  `json:"type"`
	Content     string           `json:"content"`     // the actual improvement text
	Context     string           `json:"context"`     // what triggered this improvement
	Confidence  float64          `json:"confidence"`  // 0.0-1.0 how confident we are
	Status      string           `json:"status"`      // "suggested", "applied", "rejected"
	CreatedAt   time.Time        `json:"created_at"`
	AppliedAt   *time.Time       `json:"applied_at,omitempty"`
}

// SkillPerformance tracks how a skill performs over time.
type SkillPerformance struct {
	SkillName      string    `json:"skill_name"`
	TotalUses      int       `json:"total_uses"`
	SuccessCount   int       `json:"success_count"`
	FailureCount   int       `json:"failure_count"`
	AvgDuration    float64   `json:"avg_duration_ms"`
	LastUsed       time.Time `json:"last_used"`
	Improvements   int       `json:"improvements_applied"`
	Version        int       `json:"version"`
}

// ImproveConfig configures the self-improvement system.
type ImproveConfig struct {
	Enabled           bool    `json:"enabled"`
	MinSuccessRatio   float64 `json:"min_success_ratio"`   // min ratio to suggest improvements
	MinUsesBeforeImprove int  `json:"min_uses_before_improve"` // min uses before suggesting
	MaxImprovements   int     `json:"max_improvements"`     // max pending improvements per skill
}

// DefaultImproveConfig returns sensible defaults.
func DefaultImproveConfig() ImproveConfig {
	return ImproveConfig{
		Enabled:             true,
		MinSuccessRatio:     0.6,
		MinUsesBeforeImprove: 3,
		MaxImprovements:     5,
	}
}

// SkillImprover manages the self-improvement lifecycle for skills.
type SkillImprover struct {
	config       ImproveConfig
	workspace    string
	performance  map[string]*SkillPerformance
	improvements []Improvement
	mu           sync.RWMutex
}

// NewSkillImprover creates a new SkillImprover.
func NewSkillImprover(workspace string, config ImproveConfig) *SkillImprover {
	return &SkillImprover{
		config:       config,
		workspace:    workspace,
		performance:  make(map[string]*SkillPerformance),
		improvements: make([]Improvement, 0),
	}
}

// RecordUse records a skill usage event.
func (si *SkillImprover) RecordUse(skillName string, success bool, durationMs float64) {
	if !si.config.Enabled {
		return
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	perf, ok := si.performance[skillName]
	if !ok {
		perf = &SkillPerformance{
			SkillName: skillName,
			Version:   1,
		}
		si.performance[skillName] = perf
	}

	perf.TotalUses++
	if success {
		perf.SuccessCount++
	} else {
		perf.FailureCount++
	}

	// Running average duration
	perf.AvgDuration = (perf.AvgDuration*float64(perf.TotalUses-1) + durationMs) / float64(perf.TotalUses)
	perf.LastUsed = time.Now()
}

// ShouldImprove checks if a skill is ready for improvement suggestions.
func (si *SkillImprover) ShouldImprove(skillName string) bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	perf, ok := si.performance[skillName]
	if !ok || perf.TotalUses < si.config.MinUsesBeforeImprove {
		return false
	}

	ratio := float64(perf.SuccessCount) / float64(perf.TotalUses)
	if ratio < si.config.MinSuccessRatio {
		return false
	}

	// Check pending improvements count
	pending := 0
	for _, imp := range si.improvements {
		if imp.SkillName == skillName && imp.Status == "suggested" {
			pending++
		}
	}

	return pending < si.config.MaxImprovements
}

// SuggestImprovement creates a new improvement suggestion.
func (si *SkillImprover) SuggestImprovement(
	skillName string,
	impType ImprovementType,
	content string,
	context string,
	confidence float64,
) {
	si.mu.Lock()
	defer si.mu.Unlock()

	imp := Improvement{
		ID:         fmt.Sprintf("imp_%d", time.Now().UnixNano()),
		SkillName:  skillName,
		Type:       impType,
		Content:    content,
		Context:    context,
		Confidence: confidence,
		Status:     "suggested",
		CreatedAt:  time.Now(),
	}

	si.improvements = append(si.improvements, imp)

	logger.InfoCF("skillimprove", "Improvement suggested", map[string]interface{}{
		"skill":  skillName,
		"type":   string(impType),
		"conf":   confidence,
	})
}

// GenerateImprovementPrompt builds an LLM prompt for analyzing skill usage.
func (si *SkillImprover) GenerateImprovementPrompt(
	skillName string,
	skillContent string,
	userMessage string,
	assistantResponse string,
	toolsUsed []string,
	success bool,
) string {
	var sb strings.Builder
	sb.WriteString("## Skill Usage Analysis\n\n")
	sb.WriteString(fmt.Sprintf("**Skill:** %s\n", skillName))
	sb.WriteString(fmt.Sprintf("**Success:** %v\n\n", success))

	sb.WriteString("### Current SKILL.md\n\n")
	sb.WriteString("```markdown\n")
	sb.WriteString(skillContent)
	sb.WriteString("\n```\n\n")

	sb.WriteString("### Usage Context\n\n")
	sb.WriteString(fmt.Sprintf("**User Request:** %s\n\n", userMessage))
	sb.WriteString(fmt.Sprintf("**Agent Response:** %s\n\n", truncate(assistantResponse, 1000)))

	if len(toolsUsed) > 0 {
		sb.WriteString("**Tools Used:**\n")
		for _, tool := range toolsUsed {
			sb.WriteString(fmt.Sprintf("- %s\n", tool))
		}
	}

	sb.WriteString("\n### Task\n\n")
	sb.WriteString("Analyze this skill usage and suggest improvements. Consider:\n")
	sb.WriteString("1. Were there steps the agent had to figure out that should be documented?\n")
	sb.WriteString("2. Was the description accurate for what the skill actually does?\n")
	sb.WriteString("3. Were there edge cases not covered?\n")
	sb.WriteString("4. What notes would help future executions?\n\n")
	sb.WriteString("Respond in JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{"improvements": [{"type": "add_step|refine_description|add_edge_case|add_notes|rework_steps", "content": "...", "confidence": 0.0-1.0}]}`)
	sb.WriteString("\n```\n")

	return sb.String()
}

// ParseImprovementResponse parses the LLM response into improvements.
func ParseImprovementResponse(content string) []Improvement {
	var improvements []Improvement

	// Try to extract JSON
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := content[jsonStart : jsonEnd+1]
		var parsed struct {
			Improvements []struct {
				Type       string  `json:"type"`
				Content    string  `json:"content"`
				Confidence float64 `json:"confidence"`
			} `json:"improvements"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			for _, p := range parsed.Improvements {
				imp := Improvement{
					ID:         fmt.Sprintf("imp_%d", time.Now().UnixNano()),
					Type:       ImprovementType(p.Type),
					Content:    p.Content,
					Confidence: p.Confidence,
					Status:     "suggested",
					CreatedAt:  time.Now(),
				}
				improvements = append(improvements, imp)
			}
			return improvements
		}
	}

	return improvements
}

// ApplyImprovement applies an improvement to a skill's SKILL.md file.
func (si *SkillImprover) ApplyImprovement(skillName string, improvementID string) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	// Find the improvement
	var imp *Improvement
	for i := range si.improvements {
		if si.improvements[i].ID == improvementID && si.improvements[i].SkillName == skillName {
			imp = &si.improvements[i]
			break
		}
	}

	if imp == nil {
		return fmt.Errorf("improvement %s not found for skill %s", improvementID, skillName)
	}

	if imp.Status != "suggested" {
		return fmt.Errorf("improvement %s is not in suggested status (current: %s)", improvementID, imp.Status)
	}

	// Read the current SKILL.md
	skillPath := filepath.Join(si.workspace, "skills", skillName, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	// Apply the improvement
	newContent := applyImprovementToContent(string(content), imp)

	// Write back
	if err := os.WriteFile(skillPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	// Update status
	now := time.Now()
	imp.Status = "applied"
	imp.AppliedAt = &now

	// Update performance
	if perf, ok := si.performance[skillName]; ok {
		perf.Improvements++
		perf.Version++
	}

	logger.InfoCF("skillimprove", "Improvement applied", map[string]interface{}{
		"skill":  skillName,
		"type":   string(imp.Type),
		"imp_id": improvementID,
	})

	return nil
}

// applyImprovementToContent applies an improvement to the SKILL.md content.
func applyImprovementToContent(content string, imp *Improvement) string {
	switch imp.Type {
	case AddStep:
		// Find the last numbered step and append after it
		lines := strings.Split(content, "\n")
		lastStepIdx := -1
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed, ".") {
				lastStepIdx = i
			}
		}
		if lastStepIdx >= 0 {
			// Insert after last step
			newLines := make([]string, 0, len(lines)+2)
			newLines = append(newLines, lines[:lastStepIdx+1]...)
			newLines = append(newLines, "")
			newLines = append(newLines, imp.Content)
			newLines = append(newLines, lines[lastStepIdx+1:]...)
			return strings.Join(newLines, "\n")
		}
		// No steps found, append at end
		return content + "\n\n" + imp.Content

	case RefineDesc:
		// Replace the description in frontmatter
		if strings.HasPrefix(content, "---") {
			parts := strings.SplitN(content, "---", 3)
			if len(parts) >= 3 {
				frontmatter := parts[1]
				// Find description line
				fmLines := strings.Split(frontmatter, "\n")
				for i, line := range fmLines {
					if strings.HasPrefix(strings.TrimSpace(line), "description:") {
						fmLines[i] = "description: " + imp.Content
						break
					}
				}
				return "---" + strings.Join(fmLines, "\n") + "---" + parts[2]
			}
		}
		return content

	case AddEdgeCase:
		// Find or create an Edge Cases section
		if strings.Contains(content, "## Edge Cases") {
			idx := strings.Index(content, "## Edge Cases")
			// Find the next section
			nextSection := strings.Index(content[idx+len("## Edge Cases"):], "\n## ")
			if nextSection >= 0 {
				insertAt := idx + len("## Edge Cases") + nextSection
				return content[:insertAt] + "\n" + imp.Content + content[insertAt:]
			}
			return content + "\n" + imp.Content
		}
		return content + "\n\n## Edge Cases\n\n" + imp.Content

	case AddNotes:
		// Find or create a Notes section
		if strings.Contains(content, "## Notes") {
			idx := strings.Index(content, "## Notes")
			nextSection := strings.Index(content[idx+len("## Notes"):], "\n## ")
			if nextSection >= 0 {
				insertAt := idx + len("## Notes") + nextSection
				return content[:insertAt] + "\n" + imp.Content + content[insertAt:]
			}
			return content + "\n" + imp.Content
		}
		return content + "\n\n## Notes\n\n" + imp.Content

	case ReworkSteps:
		// Replace entire Steps section
		if strings.Contains(content, "## Steps") {
			idx := strings.Index(content, "## Steps")
			nextSection := strings.Index(content[idx+len("## Steps"):], "\n## ")
			if nextSection >= 0 {
				insertAt := idx + len("## Steps") + nextSection
				return content[:idx] + "## Steps\n\n" + imp.Content + content[insertAt:]
			}
			return content[:idx] + "## Steps\n\n" + imp.Content
		}
		return content + "\n\n## Steps\n\n" + imp.Content

	default:
		return content
	}
}

// GetImprovements returns all improvements.
func (si *SkillImprover) GetImprovements() []Improvement {
	si.mu.RLock()
	defer si.mu.RUnlock()
	result := make([]Improvement, len(si.improvements))
	copy(result, si.improvements)
	return result
}

// GetPendingImprovements returns improvements with status "suggested".
func (si *SkillImprover) GetPendingImprovements(skillName string) []Improvement {
	si.mu.RLock()
	defer si.mu.RUnlock()
	var result []Improvement
	for _, imp := range si.improvements {
		if imp.SkillName == skillName && imp.Status == "suggested" {
			result = append(result, imp)
		}
	}
	return result
}

// GetPerformance returns performance stats for a skill.
func (si *SkillImprover) GetPerformance(skillName string) *SkillPerformance {
	si.mu.RLock()
	defer si.mu.RUnlock()
	perf, ok := si.performance[skillName]
	if !ok {
		return nil
	}
	copy := *perf
	return &copy
}

// GetAllPerformance returns all skill performance stats.
func (si *SkillImprover) GetAllPerformance() []*SkillPerformance {
	si.mu.RLock()
	defer si.mu.RUnlock()
	result := make([]*SkillPerformance, 0, len(si.performance))
	for _, p := range si.performance {
		copy := *p
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalUses > result[j].TotalUses
	})
	return result
}

// RejectImprovement marks an improvement as rejected.
func (si *SkillImprover) RejectImprovement(skillName string, improvementID string) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	for i := range si.improvements {
		if si.improvements[i].ID == improvementID && si.improvements[i].SkillName == skillName {
			if si.improvements[i].Status != "suggested" {
				return fmt.Errorf("improvement is not in suggested status")
			}
			si.improvements[i].Status = "rejected"
			return nil
		}
	}
	return fmt.Errorf("improvement not found")
}

// Save persists the improvement state to disk.
func (si *SkillImprover) Save() error {
	stateDir := filepath.Join(si.workspace, "state", "skillimprove")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	// Save performance
	perfData, err := json.MarshalIndent(si.performance, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "performance.json"), perfData, 0644); err != nil {
		return err
	}

	// Save improvements
	impData, err := json.MarshalIndent(si.improvements, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "improvements.json"), impData, 0644)
}

// Load restores state from disk.
func (si *SkillImprover) Load() error {
	stateDir := filepath.Join(si.workspace, "state", "skillimprove")

	// Load performance
	if data, err := os.ReadFile(filepath.Join(stateDir, "performance.json")); err == nil {
		var perf map[string]*SkillPerformance
		if json.Unmarshal(data, &perf) == nil {
			si.mu.Lock()
			si.performance = perf
			si.mu.Unlock()
		}
	}

	// Load improvements
	if data, err := os.ReadFile(filepath.Join(stateDir, "improvements.json")); err == nil {
		var imps []Improvement
		if json.Unmarshal(data, &imps) == nil {
			si.mu.Lock()
			si.improvements = imps
			si.mu.Unlock()
		}
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
