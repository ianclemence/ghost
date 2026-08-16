// Package reasoning implements reasoning chain tracking.
// It extracts, persists, and analyzes chain-of-thought reasoning from LLM
// responses. This provides observability into how the model thinks through
// problems and enables retrospective analysis of reasoning quality.
//
// Reasoning types:
//   - explicit: reasoning content from models that expose it (e.g., reasoning_content)
//   - implicit: extracted from response patterns (step-by-step markers)
//   - tool_reasoning: reasoning about tool selection and usage
package reasoning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

// ReasoningType classifies the source of reasoning.
type ReasoningType string

const (
	Explicit   ReasoningType = "explicit"   // from reasoning_content field
	Implicit   ReasoningType = "implicit"   // extracted from response text
	ToolReason ReasoningType = "tool_reason" // reasoning about tool selection
)

// ChainStep represents one step in a reasoning chain.
type ChainStep struct {
	Step        int           `json:"step"`
	Type        ReasoningType `json:"type"`
	Content     string        `json:"content"`
	Confidence  float64       `json:"confidence"` // 0.0-1.0
	Duration    time.Duration `json:"duration_ms"`
}

// ReasoningChain is a complete reasoning trace for one response.
type ReasoningChain struct {
	ID            string        `json:"id"`
	SessionKey    string        `json:"session_key"`
	TurnIndex     int           `json:"turn_index"`
	UserMessage   string        `json:"user_message"`
	Response      string        `json:"response"`
	Steps         []ChainStep   `json:"steps"`
	TotalSteps    int           `json:"total_steps"`
	HasExplicit   bool          `json:"has_explicit"`   // model provided reasoning_content
	HasImplicit   bool          `json:"has_implicit"`   // extracted from response
	HasToolReason bool          `json:"has_tool_reason"` // about tool selection
	Model         string        `json:"model"`
	Provider      string        `json:"provider"`
	Timestamp     time.Time     `json:"timestamp"`
}

// ReasoningStats holds aggregate reasoning statistics.
type ReasoningStats struct {
	TotalChains    int     `json:"total_chains"`
	AvgSteps       float64 `json:"avg_steps"`
	ExplicitCount  int     `json:"explicit_count"`
	ImplicitCount  int     `json:"implicit_count"`
	ToolReasonCount int    `json:"tool_reason_count"`
	AvgConfidence  float64 `json:"avg_confidence"`
}

// ReasoningConfig configures reasoning tracking.
type ReasoningConfig struct {
	Enabled         bool `json:"enabled"`
	MaxChains       int  `json:"max_chains"`
	ExtractImplicit bool `json:"extract_implicit"` // extract from response text
	MinStepLength   int  `json:"min_step_length"`   // min chars for a reasoning step
}

// DefaultReasoningConfig returns sensible defaults.
func DefaultReasoningConfig() ReasoningConfig {
	return ReasoningConfig{
		Enabled:         true,
		MaxChains:       200,
		ExtractImplicit: true,
		MinStepLength:   20,
	}
}

// ReasoningTracker extracts and persists reasoning chains.
type ReasoningTracker struct {
	config    ReasoningConfig
	workspace string
	chains    []ReasoningChain
	mu        sync.RWMutex
}

// NewReasoningTracker creates a new ReasoningTracker.
func NewReasoningTracker(workspace string, config ReasoningConfig) *ReasoningTracker {
	return &ReasoningTracker{
		config:    config,
		workspace: workspace,
		chains:    make([]ReasoningChain, 0),
	}
}

// TrackTurn extracts reasoning from a completed agent turn.
func (rt *ReasoningTracker) TrackTurn(
	sessionKey string,
	turnIndex int,
	userMessage string,
	response string,
	reasoningContent string, // from provider's reasoning_content field
	messages []providers.Message,
	model string,
	provider string,
) *ReasoningChain {
	if !rt.config.Enabled {
		return nil
	}

	chain := &ReasoningChain{
		ID:          fmt.Sprintf("reason_%d", time.Now().UnixNano()),
		SessionKey:  sessionKey,
		TurnIndex:   turnIndex,
		UserMessage: truncateStr(userMessage, 500),
		Response:    truncateStr(response, 1000),
		Model:       model,
		Provider:    provider,
		Timestamp:   time.Now(),
	}

	step := 0

	// Extract explicit reasoning from reasoning_content
	if reasoningContent != "" {
		chain.HasExplicit = true
		steps := extractExplicitReasoning(reasoningContent, &step, rt.config.MinStepLength)
		chain.Steps = append(chain.Steps, steps...)
	}

	// Extract implicit reasoning from response text
	if rt.config.ExtractImplicit {
		implicitSteps := extractImplicitReasoning(response, &step, rt.config.MinStepLength)
		if len(implicitSteps) > 0 {
			chain.HasImplicit = true
			chain.Steps = append(chain.Steps, implicitSteps...)
		}
	}

	// Extract tool reasoning from message history
	toolSteps := extractToolReasoning(messages, &step)
	if len(toolSteps) > 0 {
		chain.HasToolReason = true
		chain.Steps = append(chain.Steps, toolSteps...)
	}

	chain.TotalSteps = len(chain.Steps)

	// Store
	rt.mu.Lock()
	rt.chains = append(rt.chains, *chain)
	if len(rt.chains) > rt.config.MaxChains {
		rt.chains = rt.chains[len(rt.chains)-rt.config.MaxChains:]
	}
	rt.mu.Unlock()

	logger.DebugCF("reasoning", "Reasoning chain tracked", map[string]interface{}{
		"id":      chain.ID,
		"steps":   chain.TotalSteps,
		"explicit": chain.HasExplicit,
		"implicit": chain.HasImplicit,
	})

	return chain
}

// extractExplicitReasoning parses reasoning_content into chain steps.
func extractExplicitReasoning(content string, stepCounter *int, minLen int) []ChainStep {
	var steps []ChainStep

	// Split by common reasoning markers
	lines := strings.Split(content, "\n")
	currentStep := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(currentStep) >= minLen {
				*stepCounter++
				steps = append(steps, ChainStep{
					Step:       *stepCounter,
					Type:       Explicit,
					Content:    currentStep,
					Confidence: 0.9, // explicit reasoning is high confidence
				})
			}
			currentStep = ""
			continue
		}
		if currentStep != "" {
			currentStep += "\n"
		}
		currentStep += line
	}

	// Don't forget the last step
	if len(currentStep) >= minLen {
		*stepCounter++
		steps = append(steps, ChainStep{
			Step:       *stepCounter,
			Type:       Explicit,
			Content:    currentStep,
			Confidence: 0.9,
		})
	}

	return steps
}

// stepPatterns matches common reasoning patterns in responses.
var stepPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:first|step\s*\d+|initially)[,:]?\s*(.+?)(?:\n|$)`),
	regexp.MustCompile(`(?i)(?:then|next|second|step\s*\d+)[,:]?\s*(.+?)(?:\n|$)`),
	regexp.MustCompile(`(?i)(?:finally|lastly|in conclusion)[,:]?\s*(.+?)(?:\n|$)`),
	regexp.MustCompile(`(?i)(?:because|since|the reason is)[,:]?\s*(.+?)(?:\n|$)`),
	regexp.MustCompile(`(?i)(?:therefore|thus|so|consequently)[,:]?\s*(.+?)(?:\n|$)`),
	regexp.MustCompile(`(?i)(?:let me|I need to|I should|I'll)[,:]?\s*(.+?)(?:\n|$)`),
}

// extractImplicitReasoning extracts reasoning patterns from response text.
func extractImplicitReasoning(response string, stepCounter *int, minLen int) []ChainStep {
	var steps []ChainStep
	seen := make(map[string]bool)

	for _, pattern := range stepPatterns {
		matches := pattern.FindAllStringSubmatch(response, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			content := strings.TrimSpace(match[1])
			if len(content) < minLen {
				continue
			}
			// Deduplicate
			if seen[content] {
				continue
			}
			seen[content] = true

			*stepCounter++
			steps = append(steps, ChainStep{
				Step:       *stepCounter,
				Type:       Implicit,
				Content:    content,
				Confidence: 0.6, // implicit is lower confidence
			})
		}
	}

	return steps
}

// extractToolReasoning extracts reasoning about tool usage from message history.
func extractToolReasoning(messages []providers.Message, stepCounter *int) []ChainStep {
	var steps []ChainStep

	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				toolName := "unknown"
				if tc.Function != nil {
					toolName = tc.Function.Name
				}
				// Look for reasoning before tool calls (in the content)
				if msg.Content != "" {
					content := strings.TrimSpace(msg.Content)
					if len(content) > 20 {
						*stepCounter++
						steps = append(steps, ChainStep{
							Step:       *stepCounter,
							Type:       ToolReason,
							Content:    fmt.Sprintf("Choosing to use %s: %s", toolName, truncateStr(content, 200)),
							Confidence: 0.7,
						})
					}
				}
			}
		}
	}

	return steps
}

// GetChains returns all stored reasoning chains.
func (rt *ReasoningTracker) GetChains() []ReasoningChain {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	result := make([]ReasoningChain, len(rt.chains))
	copy(result, rt.chains)
	return result
}

// GetRecentChains returns the N most recent chains.
func (rt *ReasoningTracker) GetRecentChains(n int) []ReasoningChain {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if n > len(rt.chains) {
		n = len(rt.chains)
	}
	result := make([]ReasoningChain, n)
	copy(result, rt.chains[len(rt.chains)-n:])
	return result
}

// GetChainByID returns a specific chain by ID.
func (rt *ReasoningTracker) GetChainByID(id string) *ReasoningChain {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	for _, c := range rt.chains {
		if c.ID == id {
			copy := c
			return &copy
		}
	}
	return nil
}

// GetStats returns aggregate reasoning statistics.
func (rt *ReasoningTracker) GetStats() ReasoningStats {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	stats := ReasoningStats{
		TotalChains: len(rt.chains),
	}

	if len(rt.chains) == 0 {
		return stats
	}

	totalSteps := 0
	totalConfidence := 0.0
	for _, c := range rt.chains {
		totalSteps += c.TotalSteps
		if c.HasExplicit {
			stats.ExplicitCount++
		}
		if c.HasImplicit {
			stats.ImplicitCount++
		}
		if c.HasToolReason {
			stats.ToolReasonCount++
		}
		for _, s := range c.Steps {
			totalConfidence += s.Confidence
		}
	}

	totalStepsCount := 0
	for _, c := range rt.chains {
		totalStepsCount += c.TotalSteps
	}

	stats.AvgSteps = float64(totalSteps) / float64(len(rt.chains))
	if totalStepsCount > 0 {
		stats.AvgConfidence = totalConfidence / float64(totalStepsCount)
	}

	return stats
}

// GetChainsByModel returns chains filtered by model.
func (rt *ReasoningTracker) GetChainsByModel(model string) []ReasoningChain {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []ReasoningChain
	for _, c := range rt.chains {
		if c.Model == model {
			result = append(result, c)
		}
	}
	return result
}

// GetExplicitReasoningOnly returns chains that have explicit reasoning.
func (rt *ReasoningTracker) GetExplicitReasoningOnly() []ReasoningChain {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []ReasoningChain
	for _, c := range rt.chains {
		if c.HasExplicit {
			result = append(result, c)
		}
	}
	return result
}

// GetTopReasoningPatterns extracts the most common reasoning patterns.
func (rt *ReasoningTracker) GetTopReasoningPatterns(n int) []map[string]interface{} {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	freq := make(map[string]int)
	for _, c := range rt.chains {
		for _, step := range c.Steps {
			// Extract first sentence as pattern
			sentences := strings.SplitN(step.Content, ".", 2)
			if len(sentences) > 0 {
				pattern := strings.TrimSpace(sentences[0])
				if len(pattern) > 50 {
					pattern = pattern[:50]
				}
				freq[pattern]++
			}
		}
	}

	type pat struct {
		pattern string
		count   int
	}
	var sorted []pat
	for p, count := range freq {
		sorted = append(sorted, pat{p, count})
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
			"pattern": sorted[i].pattern,
			"count":   sorted[i].count,
		}
	}
	return result
}

// Save persists reasoning chains to disk.
func (rt *ReasoningTracker) Save() error {
	stateDir := filepath.Join(rt.workspace, "state", "reasoning")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	rt.mu.RLock()
	data, err := json.MarshalIndent(rt.chains, "", "  ")
	rt.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(stateDir, "chains.json"), data, 0644)
}

// Load restores reasoning chains from disk.
func (rt *ReasoningTracker) Load() error {
	path := filepath.Join(rt.workspace, "state", "reasoning", "chains.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	return json.Unmarshal(data, &rt.chains)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
