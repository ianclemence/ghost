package review

import (
	"context"
	"os"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

type mockProvider struct {
	response string
}

func (m *mockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content: m.response,
	}, nil
}

func (m *mockProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestDefaultReviewConfig(t *testing.T) {
	cfg := DefaultReviewConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.ReviewThreshold != 0.9 {
		t.Errorf("default threshold should be 0.9, got %f", cfg.ReviewThreshold)
	}
	if cfg.MaxReviews != 100 {
		t.Errorf("default max reviews should be 100, got %d", cfg.MaxReviews)
	}
}

func TestReviewer_DisabledNoOp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	cfg.Enabled = false

	reviewer := NewReviewer(tmpDir, cfg)
	reviewer.ReviewTurn(context.Background(), "session1", 0, "hello", "hi", nil, &mockProvider{}, "model")

	// Should not have any reviews
	reviews := reviewer.GetReviews()
	if len(reviews) != 0 {
		t.Error("expected no reviews when disabled")
	}
}

func TestReviewer_RuleBasedReview_EmptyResponse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	result := &ReviewResult{
		UserMessage:  "What is Go?",
		AssistantMsg: "",
		ToolsUsed:    nil,
		ToolCount:    0,
	}

	result = reviewer.ruleBasedReview(result)

	if len(result.Findings) == 0 {
		t.Error("expected findings for empty response")
	}

	found := false
	for _, f := range result.Findings {
		if f.Category == CategoryCompleteness && f.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected completeness warning for empty response")
	}
}

func TestReviewer_RuleBasedReview_ExcessiveTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	toolsUsed := []string{"web_search", "web_fetch", "read_file", "write_file", "edit_file", "exec"}
	result := &ReviewResult{
		UserMessage:  "hello",
		AssistantMsg: "response",
		ToolsUsed:    toolsUsed,
		ToolCount:    len(toolsUsed),
	}

	result = reviewer.ruleBasedReview(result)

	found := false
	for _, f := range result.Findings {
		if f.Category == CategoryEfficiency {
			found = true
		}
	}
	if !found {
		t.Error("expected efficiency finding for excessive tool usage")
	}
}

func TestReviewer_RuleBasedReview_ShortResponseToComplexQuestion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	longQuestion := "Can you explain in detail how the Go garbage collector works, including the tri-color mark and sweep algorithm, write barriers, and how it handles concurrent marking? I need a thorough explanation with examples and comparisons to other languages."
	shortResponse := "It collects garbage."

	result := &ReviewResult{
		UserMessage:  longQuestion,
		AssistantMsg: shortResponse,
		ToolCount:    0,
	}

	result = reviewer.ruleBasedReview(result)

	found := false
	for _, f := range result.Findings {
		if f.Category == CategoryCompleteness && f.Severity == SeveritySuggest {
			found = true
		}
	}
	if !found {
		t.Error("expected completeness suggestion for short response to complex question")
	}
}

func TestReviewer_RuleBasedReview_Score(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	// Perfect response
	result := &ReviewResult{
		UserMessage:  "hi",
		AssistantMsg: "Hello! How can I help?",
		ToolCount:    0,
	}
	result = reviewer.ruleBasedReview(result)
	if result.Score < 0.5 {
		t.Errorf("perfect response score should be >= 0.5, got %f", result.Score)
	}

	// Empty response
	result2 := &ReviewResult{
		UserMessage:  "complex question with lots of detail",
		AssistantMsg: "",
		ToolCount:    0,
	}
	result2 = reviewer.ruleBasedReview(result2)
	if result2.Score >= 0.7 {
		t.Errorf("empty response score should be < 0.7, got %f", result2.Score)
	}
}

func TestReviewer_ReviewTurn_Async(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	provider := &mockProvider{response: `{"score": 0.8, "findings": [], "suggestions": ["good"]}`}
	reviewer := NewReviewer(tmpDir, cfg)

	reviewer.ReviewTurn(context.Background(), "session1", 0, "hello", "hi there", []string{"web_search"}, provider, "model")

	// Wait briefly for async review
	done := make(chan bool, 1)
	go func() {
		for i := 0; i < 50; i++ {
			if len(reviewer.GetReviews()) > 0 {
				done <- true
				return
			}
		}
		done <- false
	}()
	if <-done {
		reviews := reviewer.GetReviews()
		if len(reviews) != 1 {
			t.Fatalf("expected 1 review, got %d", len(reviews))
		}
		if reviews[0].SessionKey != "session1" {
			t.Errorf("expected session key 'session1', got %s", reviews[0].SessionKey)
		}
		if reviews[0].Score != 0.8 {
			t.Errorf("expected score 0.8, got %f", reviews[0].Score)
		}
	}
}

func TestParseReviewResponse_JSON(t *testing.T) {
	content := `{
		"score": 0.85,
		"findings": [
			{"category": "completeness", "severity": "suggest", "message": "Could include more examples"}
		],
		"suggestions": ["Add code examples"]
	}`

	result := parseReviewResponse(content)
	if result.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", result.Score)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Category != CategoryCompleteness {
		t.Errorf("expected category completeness, got %s", result.Findings[0].Category)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(result.Suggestions))
	}
}

func TestParseReviewResponse_MarkdownWrappedJSON(t *testing.T) {
	content := "Here is my review:\n```json\n{\"score\": 0.7, \"findings\": [], \"suggestions\": []}\n```"

	result := parseReviewResponse(content)
	if result.Score != 0.7 {
		t.Errorf("expected score 0.7, got %f", result.Score)
	}
}

func TestParseReviewResponse_Fallback(t *testing.T) {
	content := "score: 0.6\nThis is a freeform review.\nNo structured data."

	result := parseReviewResponse(content)
	if result.Score != 0.6 {
		t.Errorf("expected score 0.6 from fallback, got %f", result.Score)
	}
}

func TestReviewer_AverageScore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	// No reviews
	if avg := reviewer.AverageScore(); avg != 0 {
		t.Errorf("average with no reviews should be 0, got %f", avg)
	}

	// Manually add reviews
	reviewer.mu.Lock()
	reviewer.reviews = append(reviewer.reviews, ReviewResult{Score: 0.8})
	reviewer.reviews = append(reviewer.reviews, ReviewResult{Score: 0.6})
	reviewer.reviews = append(reviewer.reviews, ReviewResult{Score: 1.0})
	reviewer.mu.Unlock()

	avg := reviewer.AverageScore()
	if avg < 0.79 || avg > 0.81 {
		t.Errorf("expected average ~0.8, got %f", avg)
	}
}

func TestReviewer_GetFindingsByCategory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	reviewer.mu.Lock()
	reviewer.reviews = append(reviewer.reviews, ReviewResult{
		Findings: []Finding{
			{Category: CategoryCompleteness, Severity: SeverityWarning, Message: "test1"},
			{Category: CategoryCompleteness, Severity: SeveritySuggest, Message: "test2"},
			{Category: CategoryEfficiency, Severity: SeverityInfo, Message: "test3"},
		},
	})
	reviewer.mu.Unlock()

	categories := reviewer.GetFindingsByCategory()
	if categories[CategoryCompleteness] != 2 {
		t.Errorf("expected 2 completeness findings, got %d", categories[CategoryCompleteness])
	}
	if categories[CategoryEfficiency] != 1 {
		t.Errorf("expected 1 efficiency finding, got %d", categories[CategoryEfficiency])
	}
}

func TestReviewer_GetRecentReviews(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	reviewer := NewReviewer(tmpDir, cfg)

	reviewer.mu.Lock()
	for i := 0; i < 5; i++ {
		reviewer.reviews = append(reviewer.reviews, ReviewResult{ID: string(rune('a' + i))})
	}
	reviewer.mu.Unlock()

	recent := reviewer.GetRecentReviews(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent reviews, got %d", len(recent))
	}
	if recent[0].ID != "c" {
		t.Errorf("expected first recent to be 'c', got %s", recent[0].ID)
	}
}

func TestReviewer_MaxReviews(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "review-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReviewConfig()
	cfg.MaxReviews = 3
	provider := &mockProvider{response: `{"score": 0.7, "findings": [], "suggestions": []}`}
	reviewer := NewReviewer(tmpDir, cfg)

	// Add 5 reviews
	for i := 0; i < 5; i++ {
		reviewer.ReviewTurn(context.Background(), "s", i, "q", "a", nil, provider, "m")
	}

	// Wait for all async reviews
	done := make(chan bool, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if len(reviewer.GetReviews()) >= 3 {
				done <- true
				return
			}
		}
		done <- false
	}()
	<-done

	reviews := reviewer.GetReviews()
	if len(reviews) > 3 {
		t.Errorf("expected at most 3 reviews (MaxReviews), got %d", len(reviews))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	prompt := buildReviewPrompt("user question", "assistant answer", []string{"web_search", "read_file"})
	if prompt == "" {
		t.Error("review prompt should not be empty")
	}
	if !containsStr(prompt, "user question") {
		t.Error("review prompt should contain user message")
	}
	if !containsStr(prompt, "web_search") {
		t.Error("review prompt should contain tools used")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
