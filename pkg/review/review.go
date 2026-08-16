// Package review implements autonomous background self-review.
// After each agent turn, a lightweight review pass critiques the response
// quality and logs improvement suggestions. The review runs asynchronously
// so it doesn't block the user.
//
// The review system evaluates:
// - Response completeness (did it answer the question?)
// - Tool usage efficiency (optimal tool sequence?)
// - Memory relevance (should anything be remembered?)
// - Skill applicability (should a new skill be created?)
package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

// ReviewSeverity indicates how important a finding is.
type ReviewSeverity string

const (
	SeverityInfo     ReviewSeverity = "info"
	SeveritySuggest  ReviewSeverity = "suggest"
	SeverityWarning  ReviewSeverity = "warning"
)

// ReviewCategory classifies the type of finding.
type ReviewCategory string

const (
	CategoryCompleteness ReviewCategory = "completeness"
	CategoryEfficiency   ReviewCategory = "efficiency"
	CategoryMemory       ReviewCategory = "memory"
	CategorySkill        ReviewCategory = "skill"
	CategoryAccuracy     ReviewCategory = "accuracy"
)

// Finding represents one review finding.
type Finding struct {
	Category ReviewCategory `json:"category"`
	Severity ReviewSeverity `json:"severity"`
	Message  string         `json:"message"`
}

// ReviewResult holds the full review output for a single turn.
type ReviewResult struct {
	ID            string              `json:"id"`
	TurnIndex     int                 `json:"turn_index"`
	SessionKey    string              `json:"session_key"`
	UserMessage   string              `json:"user_message"`
	AssistantMsg  string              `json:"assistant_message"`
	ToolsUsed     []string            `json:"tools_used"`
	ToolCount     int                 `json:"tool_count"`
	Findings      []Finding           `json:"findings"`
	Score         float64             `json:"score"` // 0.0 - 1.0
	Suggestions   []string            `json:"suggestions"`
	Timestamp     time.Time           `json:"timestamp"`
	ReviewLatency time.Duration       `json:"review_latency"`
}

// ReviewConfig configures the self-review system.
type ReviewConfig struct {
	Enabled         bool    `json:"enabled"`
	ReviewThreshold float64 `json:"review_threshold"` // min score to skip review (0.0-1.0)
	MaxReviews      int     `json:"max_reviews"`      // max reviews to keep in history
}

// DefaultReviewConfig returns sensible defaults.
func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{
		Enabled:         true,
		ReviewThreshold: 0.9, // skip review if response is very high quality
		MaxReviews:      100,
	}
}

// Reviewer performs autonomous background reviews of agent turns.
type Reviewer struct {
	config  ReviewConfig
	workspace string
	reviews []ReviewResult
	mu      sync.RWMutex
}

// NewReviewer creates a new Reviewer.
func NewReviewer(workspace string, config ReviewConfig) *Reviewer {
	return &Reviewer{
		config:    config,
		workspace: workspace,
		reviews:   make([]ReviewResult, 0),
	}
}

// ReviewTurn reviews a completed agent turn asynchronously.
// The review runs in a goroutine so it doesn't block the user.
func (r *Reviewer) ReviewTurn(
	ctx context.Context,
	sessionKey string,
	turnIndex int,
	userMessage string,
	assistantMessage string,
	toolsUsed []string,
	provider providers.LLMProvider,
	model string,
) {
	if !r.config.Enabled {
		return
	}

	go func() {
		start := time.Now()
		result := r.performReview(ctx, sessionKey, turnIndex, userMessage, assistantMessage, toolsUsed, provider, model)
		result.ReviewLatency = time.Since(start)

		r.mu.Lock()
		r.reviews = append(r.reviews, *result)
		// Trim to max
		if len(r.reviews) > r.config.MaxReviews {
			r.reviews = r.reviews[len(r.reviews)-r.config.MaxReviews:]
		}
		r.mu.Unlock()

		// Persist asynchronously
		if err := r.save(); err != nil {
			logger.DebugCF("review", "Failed to save reviews", map[string]interface{}{
				"error": err.Error(),
			})
		}

		logger.DebugCF("review", "Turn review completed", map[string]interface{}{
			"score":    result.Score,
			"findings": len(result.Findings),
			"latency":  result.ReviewLatency.Milliseconds(),
		})
	}()
}

// performReview executes the actual review logic.
func (r *Reviewer) performReview(
	ctx context.Context,
	sessionKey string,
	turnIndex int,
	userMessage string,
	assistantMessage string,
	toolsUsed []string,
	provider providers.LLMProvider,
	model string,
) *ReviewResult {
	result := &ReviewResult{
		ID:           fmt.Sprintf("rev_%d", time.Now().UnixNano()),
		TurnIndex:    turnIndex,
		SessionKey:   sessionKey,
		UserMessage:  truncate(userMessage, 500),
		AssistantMsg: truncate(assistantMessage, 500),
		ToolsUsed:    toolsUsed,
		ToolCount:    len(toolsUsed),
		Timestamp:    time.Now(),
	}

	// Build review prompt
	reviewPrompt := buildReviewPrompt(userMessage, assistantMessage, toolsUsed)

	messages := []providers.Message{
		{Role: "system", Content: reviewSystemPrompt},
		{Role: "user", Content: reviewPrompt},
	}

	// Call LLM for review (with timeout)
	reviewCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	options := map[string]interface{}{
		"temperature": 0.3, // low temperature for consistent review
	}

	resp, err := provider.Chat(reviewCtx, messages, nil, model, options)
	if err != nil {
		logger.DebugCF("review", "Review LLM call failed", map[string]interface{}{
			"error": err.Error(),
		})
		// Fall back to rule-based review
		return r.ruleBasedReview(result)
	}

	// Parse the review response
	parsed := parseReviewResponse(resp.Content)
	result.Findings = parsed.Findings
	result.Suggestions = parsed.Suggestions
	result.Score = parsed.Score

	return result
}

// ruleBasedReview provides a fallback review when the LLM is unavailable.
func (r *Reviewer) ruleBasedReview(result *ReviewResult) *ReviewResult {
	var findings []Finding
	var suggestions []string
	score := 0.7 // default moderate score

	// Check for empty response
	if result.AssistantMsg == "" || result.AssistantMsg == "..." {
		findings = append(findings, Finding{
			Category: CategoryCompleteness,
			Severity: SeverityWarning,
			Message:  "Response is empty or minimal",
		})
		score -= 0.3
	}

	// Check for excessive tool usage
	if result.ToolCount > 5 {
		findings = append(findings, Finding{
			Category: CategoryEfficiency,
			Severity: SeveritySuggest,
			Message:  fmt.Sprintf("Used %d tools — consider consolidating tool calls", result.ToolCount),
		})
		suggestions = append(suggestions, "Consider batching related tool operations")
		score -= 0.1
	}

	// Check for no tool usage when user asked a question
	if result.ToolCount == 0 && len(result.UserMessage) > 50 {
		suggestions = append(suggestions, "Consider if tools could enhance the response")
	}

	// Check for very short response to complex question
	if len(result.UserMessage) > 200 && len(result.AssistantMsg) < 50 {
		findings = append(findings, Finding{
			Category: CategoryCompleteness,
			Severity: SeveritySuggest,
			Message:  "Response seems too short for a complex question",
		})
		score -= 0.15
	}

	// Clamp score
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	result.Findings = findings
	result.Suggestions = suggestions
	result.Score = score
	return result
}

// ReviewSystemPrompt instructs the review model.
const reviewSystemPrompt = `You are a quality reviewer for an AI agent. Your job is to analyze a user-agent interaction and provide constructive feedback.

Analyze the following aspects:
1. COMPLETENESS: Did the agent fully answer the user's question or complete the task?
2. EFFICIENCY: Did the agent use tools optimally? Were there unnecessary steps?
3. ACCURACY: Is the response factually sound? Any hallucinations?
4. MEMORY: Should the agent remember anything from this interaction?
5. SKILL: Should a new skill be created for this type of task?

Respond in this exact JSON format:
{
  "score": 0.0-1.0,
  "findings": [
    {"category": "completeness|efficiency|accuracy|memory|skill", "severity": "info|suggest|warning", "message": "..."}
  ],
  "suggestions": ["suggestion 1", "suggestion 2"]
}

Where:
- score: 0.0 (terrible) to 1.0 (perfect)
- findings: specific observations
- suggestions: actionable improvements

Be concise and constructive. Focus on the most impactful improvements.`

// buildReviewPrompt creates the prompt for the review model.
func buildReviewPrompt(userMessage, assistantMessage string, toolsUsed []string) string {
	var sb strings.Builder
	sb.WriteString("## User Message\n\n")
	sb.WriteString(userMessage)
	sb.WriteString("\n\n## Assistant Response\n\n")
	sb.WriteString(truncate(assistantMessage, 2000))

	if len(toolsUsed) > 0 {
		sb.WriteString("\n\n## Tools Used\n\n")
		for _, tool := range toolsUsed {
			sb.WriteString(fmt.Sprintf("- %s\n", tool))
		}
	}

	return sb.String()
}

// ReviewParsed holds the parsed review response.
type ReviewParsed struct {
	Score       float64
	Findings    []Finding
	Suggestions []string
}

// parseReviewResponse extracts structured data from the review LLM response.
func parseReviewResponse(content string) ReviewParsed {
	result := ReviewParsed{
		Score:       0.7,
		Findings:    []Finding{},
		Suggestions: []string{},
	}

	// Try to extract JSON from the response
	content = strings.TrimSpace(content)

	// Find JSON block (may be wrapped in ```json ... ```)
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := content[jsonStart : jsonEnd+1]
		var parsed struct {
			Score       float64   `json:"score"`
			Findings    []Finding `json:"findings"`
			Suggestions []string  `json:"suggestions"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			result.Score = parsed.Score
			result.Findings = parsed.Findings
			result.Suggestions = parsed.Suggestions
			return result
		}
	}

	// Fallback: extract score from text
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "score:") {
			scoreStr := strings.TrimSpace(line[len("score:"):])
			var s float64
			if _, err := fmt.Sscanf(scoreStr, "%f", &s); err == nil && s >= 0 && s <= 1 {
				result.Score = s
			}
		}
	}

	return result
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetReviews returns all stored reviews.
func (r *Reviewer) GetReviews() []ReviewResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ReviewResult, len(r.reviews))
	copy(result, r.reviews)
	return result
}

// GetRecentReviews returns the N most recent reviews.
func (r *Reviewer) GetRecentReviews(n int) []ReviewResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n > len(r.reviews) {
		n = len(r.reviews)
	}
	result := make([]ReviewResult, n)
	copy(result, r.reviews[len(r.reviews)-n:])
	return result
}

// AverageScore returns the mean score across all reviews.
func (r *Reviewer) AverageScore() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.reviews) == 0 {
		return 0
	}
	total := 0.0
	for _, rev := range r.reviews {
		total += rev.Score
	}
	return total / float64(len(r.reviews))
}

// GetFindingsByCategory groups findings by category.
func (r *Reviewer) GetFindingsByCategory() map[ReviewCategory]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counts := make(map[ReviewCategory]int)
	for _, rev := range r.reviews {
		for _, f := range rev.Findings {
			counts[f.Category]++
		}
	}
	return counts
}

// save persists reviews to disk.
func (r *Reviewer) save() error {
	stateDir := filepath.Join(r.workspace, "state", "review")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	r.mu.RLock()
	data, err := json.MarshalIndent(r.reviews, "", "  ")
	r.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(stateDir, "reviews.json"), data, 0644)
}

// Load restores reviews from disk.
func (r *Reviewer) Load() error {
	path := filepath.Join(r.workspace, "state", "review", "reviews.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return json.Unmarshal(data, &r.reviews)
}
