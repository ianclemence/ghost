// Package trajectory implements conversation trajectory compression.
// Trajectories are structured summaries of agent conversations that capture
// the task, actions taken, tools used, and outcomes. They serve two purposes:
//
// 1. Training data: compressed trajectories can be used to train future models
// 2. Evolution feedback: trajectories feed into the evolution system to
//    identify patterns and improve skill generation
//
// A trajectory compresses a full conversation into a compact record:
//   - Task: what the user wanted
//   - Actions: what the agent did (tool calls, reasoning)
//   - Outcome: success/failure, duration, quality score
//   - Metadata: session info, model used, token counts
package trajectory

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
	"github.com/ianclemence/ghost/pkg/providers"
)

// Outcome represents the result of a trajectory.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomePartial Outcome = "partial"
	OutcomeFailure Outcome = "failure"
)

// Action represents one step in a trajectory.
type Action struct {
	Step        int       `json:"step"`
	Type        string    `json:"type"`        // "tool_call", "reasoning", "user_input", "response"
	ToolName    string    `json:"tool_name"`   // for tool_call type
	Summary     string    `json:"summary"`     // brief description of the action
	Duration    float64   `json:"duration_ms"` // how long this step took
	TokensUsed  int       `json:"tokens_used"`
	Timestamp   time.Time `json:"timestamp"`
}

// Trajectory is a compressed record of an agent conversation.
type Trajectory struct {
	ID             string            `json:"id"`
	SessionKey     string            `json:"session_key"`
	Task           string            `json:"task"`            // what the user wanted
	TaskCategory   string            `json:"task_category"`   // classified task type
	Actions        []Action          `json:"actions"`         // steps taken
	ToolsUsed      []string          `json:"tools_used"`      // unique tools used
	ToolSequence   []string          `json:"tool_sequence"`   // ordered tool sequence
	Outcome        Outcome           `json:"outcome"`         // success/partial/failure
	QualityScore   float64           `json:"quality_score"`   // 0.0-1.0
	TotalDuration  float64           `json:"total_duration_ms"`
	TotalTokens    int               `json:"total_tokens"`
	Model          string            `json:"model"`
	Provider       string            `json:"provider"`
	UserMessage    string            `json:"user_message"`
	ResponsePreview string           `json:"response_preview"` // first 500 chars of response
	TurnCount      int               `json:"turn_count"`      // number of LLM turns
	Timestamp      time.Time         `json:"timestamp"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// CompressConfig configures trajectory compression.
type CompressConfig struct {
	Enabled            bool `json:"enabled"`
	MaxTrajectories    int  `json:"max_trajectories"`     // max to keep
	MinTurnsToCompress int  `json:"min_turns_to_compress"` // skip very short conversations
	MaxActionSummary   int  `json:"max_action_summary"`   // max chars per action summary
}

// DefaultCompressConfig returns sensible defaults.
func DefaultCompressConfig() CompressConfig {
	return CompressConfig{
		Enabled:            true,
		MaxTrajectories:    500,
		MinTurnsToCompress: 2,
		MaxActionSummary:   200,
	}
}

// Compressor compresses conversations into trajectories.
type Compressor struct {
	config      CompressConfig
	workspace   string
	trajectories []Trajectory
	mu          sync.RWMutex
}

// NewCompressor creates a new Compressor.
func NewCompressor(workspace string, config CompressConfig) *Compressor {
	return &Compressor{
		config:      config,
		workspace:   workspace,
		trajectories: make([]Trajectory, 0),
	}
}

// CompressTurn compresses a conversation turn into a trajectory.
// Call this after each completed user turn with the full conversation context.
func (c *Compressor) CompressTurn(
	sessionKey string,
	userMessage string,
	assistantMessage string,
	messages []providers.Message,
	model string,
	provider string,
	toolsUsed []string,
	totalTokens int,
	turnCount int,
) *Trajectory {
	if !c.config.Enabled {
		return nil
	}

	if turnCount < c.config.MinTurnsToCompress {
		return nil
	}

	trajectory := &Trajectory{
		ID:              fmt.Sprintf("traj_%d", time.Now().UnixNano()),
		SessionKey:      sessionKey,
		Task:            extractTask(userMessage),
		TaskCategory:    classifyTask(userMessage),
		ToolsUsed:       uniqueStrings(toolsUsed),
		ToolSequence:    toolsUsed,
		Model:           model,
		Provider:        provider,
		UserMessage:     truncateStr(userMessage, 500),
		ResponsePreview: truncateStr(assistantMessage, 500),
		TurnCount:       turnCount,
		TotalTokens:     totalTokens,
		Timestamp:       time.Now(),
	}

	// Extract actions from message history
	trajectory.Actions = extractActions(messages, c.config.MaxActionSummary)

	// Calculate total duration from actions
	totalDuration := 0.0
	for _, action := range trajectory.Actions {
		totalDuration += action.Duration
	}
	trajectory.TotalDuration = totalDuration

	// Determine outcome
	trajectory.Outcome = determineOutcome(assistantMessage, toolsUsed)

	// Calculate quality score
	trajectory.QualityScore = calculateQuality(trajectory)

	// Store
	c.mu.Lock()
	c.trajectories = append(c.trajectories, *trajectory)
	// Trim
	if len(c.trajectories) > c.config.MaxTrajectories {
		c.trajectories = c.trajectories[len(c.trajectories)-c.config.MaxTrajectories:]
	}
	c.mu.Unlock()

	logger.DebugCF("trajectory", "Trajectory compressed", map[string]interface{}{
		"id":       trajectory.ID,
		"task":     trajectory.TaskCategory,
		"outcome":  string(trajectory.Outcome),
		"actions":  len(trajectory.Actions),
		"quality":  trajectory.QualityScore,
	})

	return trajectory
}

// extractTask extracts the core task from the user message.
func extractTask(userMessage string) string {
	// Take first sentence or first 200 chars
	lines := strings.SplitN(userMessage, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])
	if len(firstLine) > 200 {
		return firstLine[:200] + "..."
	}
	return firstLine
}

// classifyTask classifies the task type from the user message.
func classifyTask(userMessage string) string {
	lower := strings.ToLower(userMessage)

	switch {
	case containsAny(lower, "search", "find", "look up", "google"):
		return "search"
	case containsAny(lower, "write", "create", "generate", "draft"):
		return "creation"
	case containsAny(lower, "read", "show", "display", "list"):
		return "retrieval"
	case containsAny(lower, "edit", "modify", "update", "change"):
		return "modification"
	case containsAny(lower, "explain", "describe", "how", "what", "why"):
		return "explanation"
	case containsAny(lower, "delete", "remove", "destroy"):
		return "deletion"
	case containsAny(lower, "run", "execute", "build", "compile"):
		return "execution"
	case containsAny(lower, "analyze", "compare", "evaluate"):
		return "analysis"
	case containsAny(lower, "summarize", "tldr", "brief"):
		return "summarization"
	case containsAny(lower, "help", "assist", "support"):
		return "assistance"
	default:
		return "general"
	}
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractActions extracts structured actions from the message history.
func extractActions(messages []providers.Message, maxSummary int) []Action {
	var actions []Action
	step := 0

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			step++
			actions = append(actions, Action{
				Step:     step,
				Type:     "user_input",
				Summary:  truncateStr(msg.Content, maxSummary),
				Timestamp: time.Now(),
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					step++
					toolName := "unknown"
					if tc.Function != nil {
						toolName = tc.Function.Name
					}
					actions = append(actions, Action{
						Step:     step,
						Type:     "tool_call",
						ToolName: toolName,
						Summary:  truncateStr(fmt.Sprintf("Called %s", toolName), maxSummary),
						Timestamp: time.Now(),
					})
				}
			} else if msg.Content != "" {
				step++
				actions = append(actions, Action{
					Step:     step,
					Type:     "response",
					Summary:  truncateStr(msg.Content, maxSummary),
					TokensUsed: len(msg.Content) / 4, // rough estimate
					Timestamp: time.Now(),
				})
			}
		}
	}

	return actions
}

// determineOutcome determines if the trajectory was successful.
func determineOutcome(assistantMessage string, toolsUsed []string) Outcome {
	lower := strings.ToLower(assistantMessage)

	// Check for error indicators
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "unable") {
		return OutcomeFailure
	}

	// Check for success indicators
	if strings.Contains(lower, "successfully") || strings.Contains(lower, "done") || strings.Contains(lower, "completed") {
		return OutcomeSuccess
	}

	// If tools were used and we got a response, assume partial success
	if len(toolsUsed) > 0 && assistantMessage != "" {
		return OutcomePartial
	}

	// Default to success if we have a non-empty response
	if assistantMessage != "" {
		return OutcomeSuccess
	}

	return OutcomeFailure
}

// calculateQuality computes a quality score for the trajectory.
func calculateQuality(t *Trajectory) float64 {
	score := 0.5 // base score

	// Outcome bonus
	switch t.Outcome {
	case OutcomeSuccess:
		score += 0.3
	case OutcomePartial:
		score += 0.1
	case OutcomeFailure:
		score -= 0.2
	}

	// Efficiency bonus (fewer actions for same task = better)
	if len(t.Actions) > 0 && len(t.Actions) <= 5 {
		score += 0.1
	} else if len(t.Actions) > 10 {
		score -= 0.1
	}

	// Tool diversity bonus (using different tools = more capable)
	uniqueTools := len(t.ToolsUsed)
	if uniqueTools >= 2 && uniqueTools <= 4 {
		score += 0.1
	}

	// Clamp to [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// uniqueStrings returns unique strings from a slice.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// truncateStr truncates a string to maxLen.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetTrajectories returns all stored trajectories.
func (c *Compressor) GetTrajectories() []Trajectory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Trajectory, len(c.trajectories))
	copy(result, c.trajectories)
	return result
}

// GetRecentTrajectories returns the N most recent trajectories.
func (c *Compressor) GetRecentTrajectories(n int) []Trajectory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n > len(c.trajectories) {
		n = len(c.trajectories)
	}
	result := make([]Trajectory, n)
	copy(result, c.trajectories[len(c.trajectories)-n:])
	return result
}

// GetByCategory returns trajectories filtered by task category.
func (c *Compressor) GetByCategory(category string) []Trajectory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []Trajectory
	for _, t := range c.trajectories {
		if t.TaskCategory == category {
			result = append(result, t)
		}
	}
	return result
}

// GetByOutcome returns trajectories filtered by outcome.
func (c *Compressor) GetByOutcome(outcome Outcome) []Trajectory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []Trajectory
	for _, t := range c.trajectories {
		if t.Outcome == outcome {
			result = append(result, t)
		}
	}
	return result
}

// GetStats returns aggregate statistics.
func (c *Compressor) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"total":       len(c.trajectories),
		"categories":  make(map[string]int),
		"outcomes":    make(map[string]int),
		"avg_quality": 0.0,
		"avg_tokens":  0,
	}

	if len(c.trajectories) == 0 {
		return stats
	}

	totalQuality := 0.0
	totalTokens := 0
	categories := stats["categories"].(map[string]int)
	outcomes := stats["outcomes"].(map[string]int)

	for _, t := range c.trajectories {
		categories[t.TaskCategory]++
		outcomes[string(t.Outcome)]++
		totalQuality += t.QualityScore
		totalTokens += t.TotalTokens
	}

	stats["avg_quality"] = totalQuality / float64(len(c.trajectories))
	stats["avg_tokens"] = totalTokens / len(c.trajectories)

	return stats
}

// GetTopTools returns the most frequently used tools across trajectories.
func (c *Compressor) GetTopTools(n int) []map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	counts := make(map[string]int)
	for _, t := range c.trajectories {
		for _, tool := range t.ToolsUsed {
			counts[tool]++
		}
	}

	type toolCount struct {
		tool  string
		count int
	}
	var sorted []toolCount
	for tool, count := range counts {
		sorted = append(sorted, toolCount{tool, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	if n > len(sorted) {
		n = len(sorted)
	}

	result := make([]map[string]interface{}, n)
	for i := 0; i < n; i++ {
		result[i] = map[string]interface{}{
			"tool":  sorted[i].tool,
			"count": sorted[i].count,
		}
	}
	return result
}

// ToJSONL exports trajectories as JSONL (one JSON object per line).
func (c *Compressor) ToJSONL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder
	for _, t := range c.trajectories {
		data, err := json.Marshal(t)
		if err != nil {
			continue
		}
		sb.Write(data)
		sb.WriteString("\n")
	}
	return sb.String()
}

// Save persists trajectories to disk.
func (c *Compressor) Save() error {
	stateDir := filepath.Join(c.workspace, "state", "trajectory")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	c.mu.RLock()
	data, err := json.MarshalIndent(c.trajectories, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(stateDir, "trajectories.json"), data, 0644)
}

// Load restores trajectories from disk.
func (c *Compressor) Load() error {
	path := filepath.Join(c.workspace, "state", "trajectory", "trajectories.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return json.Unmarshal(data, &c.trajectories)
}
