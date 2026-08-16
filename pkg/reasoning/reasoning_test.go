package reasoning

import (
	"os"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestDefaultReasoningConfig(t *testing.T) {
	cfg := DefaultReasoningConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.MaxChains != 200 {
		t.Errorf("expected MaxChains 200, got %d", cfg.MaxChains)
	}
	if !cfg.ExtractImplicit {
		t.Error("expected ExtractImplicit to be true")
	}
	if cfg.MinStepLength != 20 {
		t.Errorf("expected MinStepLength 20, got %d", cfg.MinStepLength)
	}
}

func TestReasoningTracker_DisabledNoOp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	cfg.Enabled = false
	rt := NewReasoningTracker(tmpDir, cfg)

	chain := rt.TrackTurn("s1", 0, "hello", "response", "", nil, "model", "provider")
	if chain != nil {
		t.Error("expected nil chain when disabled")
	}
	if len(rt.GetChains()) != 0 {
		t.Error("expected no chains when disabled")
	}
}

func TestReasoningTracker_ExplicitReasoning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	reasoning := "First, I need to analyze the user's request. The user wants a weather forecast for Tokyo.\n\nThen, I should check if the weather skill is available and can handle this request.\n\nFinally, I'll invoke the weather tool with the correct parameters."

	chain := rt.TrackTurn("s1", 0, "weather in Tokyo", "checking weather", reasoning, nil, "test-model", "test-provider")

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if !chain.HasExplicit {
		t.Error("expected HasExplicit to be true")
	}
	if len(chain.Steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(chain.Steps))
	}

	for _, step := range chain.Steps {
		if step.Type != Explicit {
			t.Errorf("expected Explicit type, got %s", step.Type)
		}
		if step.Confidence < 0.0 || step.Confidence > 1.0 {
			t.Errorf("confidence out of range: %f", step.Confidence)
		}
	}
}

func TestReasoningTracker_ImplicitReasoning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	response := `Let me think about this. First, I need to parse the user's input and identify the intent.
Then I'll look up the appropriate skill to handle the request.
Therefore, I'll use the web search skill to find relevant information.`

	chain := rt.TrackTurn("s1", 0, "search something", response, "", nil, "test-model", "test-provider")

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if !chain.HasImplicit {
		t.Error("expected HasImplicit to be true")
	}
	if len(chain.Steps) < 1 {
		t.Errorf("expected at least 1 implicit step, got %d", len(chain.Steps))
	}

	for _, step := range chain.Steps {
		if step.Type != Implicit {
			t.Errorf("expected Implicit type, got %s", step.Type)
		}
	}
}

func TestReasoningTracker_ToolReasoning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	messages := []providers.Message{
		{Role: "user", Content: "weather in Tokyo"},
		{
			Role:    "assistant",
			Content: "I need to check the weather for Tokyo using the weather tool",
			ToolCalls: []providers.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: &providers.FunctionCall{
						Name:      "weather",
						Arguments: `{"city":"Tokyo"}`,
					},
				},
			},
		},
	}

	chain := rt.TrackTurn("s1", 0, "weather in Tokyo", "sunny", "", messages, "test-model", "test-provider")

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if !chain.HasToolReason {
		t.Error("expected HasToolReason to be true")
	}

	toolSteps := 0
	for _, step := range chain.Steps {
		if step.Type == ToolReason {
			toolSteps++
		}
	}
	if toolSteps < 1 {
		t.Errorf("expected at least 1 tool reason step, got %d", toolSteps)
	}
}

func TestReasoningTracker_NoReasoning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	chain := rt.TrackTurn("s1", 0, "hello", "hi there", "", nil, "test-model", "test-provider")

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if chain.TotalSteps != 0 {
		t.Errorf("expected 0 steps for simple response, got %d", chain.TotalSteps)
	}
}

func TestReasoningTracker_MaxChains(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	cfg.MaxChains = 3
	rt := NewReasoningTracker(tmpDir, cfg)

	for i := 0; i < 5; i++ {
		rt.TrackTurn("s1", i, "hello", "hi", "", nil, "model", "provider")
	}

	if len(rt.GetChains()) != 3 {
		t.Errorf("expected max 3 chains, got %d", len(rt.GetChains()))
	}
}

func TestReasoningTracker_GetRecentChains(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	for i := 0; i < 5; i++ {
		rt.TrackTurn("s1", i, "hello", "hi", "", nil, "model", "provider")
	}

	recent := rt.GetRecentChains(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent chains, got %d", len(recent))
	}

	// Should be the last 3
	if recent[0].TurnIndex != 2 {
		t.Errorf("expected first recent chain to have TurnIndex 2, got %d", recent[0].TurnIndex)
	}
}

func TestReasoningTracker_GetChainByID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	chain := rt.TrackTurn("s1", 0, "hello", "hi", "", nil, "model", "provider")
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}

	found := rt.GetChainByID(chain.ID)
	if found == nil {
		t.Fatal("expected to find chain by ID")
	}
	if found.ID != chain.ID {
		t.Errorf("expected ID %s, got %s", chain.ID, found.ID)
	}

	notFound := rt.GetChainByID("nonexistent")
	if notFound != nil {
		t.Error("expected nil for nonexistent ID")
	}
}

func TestReasoningTracker_GetStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	// Empty stats
	stats := rt.GetStats()
	if stats.TotalChains != 0 {
		t.Errorf("expected 0 chains in empty stats, got %d", stats.TotalChains)
	}

	// Add some chains
	rt.TrackTurn("s1", 0, "hello", "hi", "First, think about this carefully. Then respond appropriately.", nil, "model", "provider")
	rt.TrackTurn("s1", 1, "hello", "hi", "", nil, "model", "provider")

	stats = rt.GetStats()
	if stats.TotalChains != 2 {
		t.Errorf("expected 2 chains, got %d", stats.TotalChains)
	}
	if stats.ExplicitCount != 1 {
		t.Errorf("expected 1 explicit, got %d", stats.ExplicitCount)
	}
	if stats.AvgSteps < 0 {
		t.Errorf("expected non-negative avg steps, got %f", stats.AvgSteps)
	}
}

func TestReasoningTracker_GetChainsByModel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	rt.TrackTurn("s1", 0, "hello", "hi", "", nil, "model-a", "provider")
	rt.TrackTurn("s1", 1, "hello", "hi", "", nil, "model-b", "provider")
	rt.TrackTurn("s1", 2, "hello", "hi", "", nil, "model-a", "provider")

	chains := rt.GetChainsByModel("model-a")
	if len(chains) != 2 {
		t.Errorf("expected 2 chains for model-a, got %d", len(chains))
	}
}

func TestReasoningTracker_GetExplicitReasoningOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	rt.TrackTurn("s1", 0, "hello", "hi", "First, analyze the request. Then provide an answer.", nil, "model", "provider")
	rt.TrackTurn("s1", 1, "hello", "hi", "", nil, "model", "provider")

	explicit := rt.GetExplicitReasoningOnly()
	if len(explicit) != 1 {
		t.Errorf("expected 1 explicit chain, got %d", len(explicit))
	}
}

func TestReasoningTracker_GetTopReasoningPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	// Add multiple chains with similar patterns
	for i := 0; i < 5; i++ {
		rt.TrackTurn("s1", i, "hello", "hi",
			"First, I need to analyze the request carefully. Then provide a response.",
			nil, "model", "provider")
	}

	patterns := rt.GetTopReasoningPatterns(3)
	if len(patterns) == 0 {
		t.Error("expected some patterns")
	}
}

func TestReasoningTracker_SaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	rt.TrackTurn("s1", 0, "hello", "hi", "First, think. Then respond.", nil, "model", "provider")
	rt.TrackTurn("s1", 1, "hello", "hi", "", nil, "model", "provider")

	if err := rt.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Load into new tracker
	rt2 := NewReasoningTracker(tmpDir, cfg)
	if err := rt2.Load(); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	chains := rt2.GetChains()
	if len(chains) != 2 {
		t.Errorf("expected 2 chains after load, got %d", len(chains))
	}
}

func TestReasoningTracker_LoadNoFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	// Loading when no file exists should be fine
	if err := rt.Load(); err != nil {
		t.Fatalf("Failed to load non-existent: %v", err)
	}
}

func TestExtractExplicitReasoning(t *testing.T) {
	content := "First, I need to check the weather.\n\nThen I'll use the tool.\n\nFinally, I'll respond."
	step := 0
	steps := extractExplicitReasoning(content, &step, 10)

	if len(steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(steps))
	}

	for _, s := range steps {
		if s.Type != Explicit {
			t.Errorf("expected Explicit type, got %s", s.Type)
		}
		if s.Step == 0 {
			t.Error("expected non-zero step number")
		}
	}
}

func TestExtractExplicitReasoning_ShortSteps(t *testing.T) {
	content := "ok"
	step := 0
	steps := extractExplicitReasoning(content, &step, 20)

	if len(steps) != 0 {
		t.Errorf("expected 0 steps for short content, got %d", len(steps))
	}
}

func TestExtractImplicitReasoning(t *testing.T) {
	response := "Let me analyze this. First, I need to understand the input. Then I'll process it."
	step := 0
	steps := extractImplicitReasoning(response, &step, 10)

	if len(steps) < 1 {
		t.Errorf("expected at least 1 step, got %d", len(steps))
	}

	for _, s := range steps {
		if s.Type != Implicit {
			t.Errorf("expected Implicit type, got %s", s.Type)
		}
	}
}

func TestExtractImplicitReasoning_NoPatterns(t *testing.T) {
	response := "The weather is nice today."
	step := 0
	steps := extractImplicitReasoning(response, &step, 10)

	if len(steps) != 0 {
		t.Errorf("expected 0 steps for simple response, got %d", len(steps))
	}
}

func TestExtractToolReasoning(t *testing.T) {
	messages := []providers.Message{
		{Role: "user", Content: "search for go tutorials"},
		{
			Role:    "assistant",
			Content: "I'll search for Go tutorials using the web search tool",
			ToolCalls: []providers.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: &providers.FunctionCall{
						Name:      "web_search",
						Arguments: `{"query":"go tutorials"}`,
					},
				},
			},
		},
	}

	step := 0
	steps := extractToolReasoning(messages, &step)

	if len(steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Type != ToolReason {
		t.Errorf("expected ToolReason type, got %s", steps[0].Type)
	}
}

func TestExtractToolReasoning_NoToolCalls(t *testing.T) {
	messages := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	step := 0
	steps := extractToolReasoning(messages, &step)

	if len(steps) != 0 {
		t.Errorf("expected 0 steps for no tool calls, got %d", len(steps))
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"hi", 2, "hi"},
		{"hello", 5, "hello"},
	}

	for _, tt := range tests {
		result := truncateStr(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestReasoningChain_Timestamp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	before := time.Now()
	chain := rt.TrackTurn("s1", 0, "hello", "hi", "", nil, "model", "provider")
	after := time.Now()

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if chain.Timestamp.Before(before) || chain.Timestamp.After(after) {
		t.Error("expected timestamp between before and after")
	}
}

func TestReasoningChain_SessionAndTurn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	chain := rt.TrackTurn("my-session", 42, "hello", "hi", "", nil, "model", "provider")

	if chain.SessionKey != "my-session" {
		t.Errorf("expected session key 'my-session', got %s", chain.SessionKey)
	}
	if chain.TurnIndex != 42 {
		t.Errorf("expected turn index 42, got %d", chain.TurnIndex)
	}
}

func TestReasoningTracker_MixedReasoningTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	reasoning := `First, I need to understand the request. This is important for getting the right answer.`

	messages := []providers.Message{
		{
			Role:    "assistant",
			Content: "I'll use the file search tool to find the relevant code",
			ToolCalls: []providers.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: &providers.FunctionCall{
						Name:      "file_search",
						Arguments: `{"pattern":"*.go"}`,
					},
				},
			},
		},
	}

	chain := rt.TrackTurn("s1", 0, "find code", "found", reasoning, messages, "model", "provider")

	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if !chain.HasExplicit {
		t.Error("expected HasExplicit")
	}
	if !chain.HasToolReason {
		t.Error("expected HasToolReason")
	}

	// Should have steps of different types
	types := make(map[ReasoningType]bool)
	for _, step := range chain.Steps {
		types[step.Type] = true
	}
	if !types[Explicit] {
		t.Error("expected Explicit steps")
	}
	if !types[ToolReason] {
		t.Error("expected ToolReason steps")
	}
}

func TestReasoningTracker_SaveCreatesDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	rt.TrackTurn("s1", 0, "hello", "hi", "", nil, "model", "provider")

	// Save should create the directory
	if err := rt.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Check that file was created
	if _, err := os.Stat(tmpDir + "/state/reasoning/chains.json"); os.IsNotExist(err) {
		t.Error("expected chains.json to exist")
	}
}

func TestReasoningTracker_GetChainsByModel_NoMatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasoning-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultReasoningConfig()
	rt := NewReasoningTracker(tmpDir, cfg)

	rt.TrackTurn("s1", 0, "hello", "hi", "", nil, "model-a", "provider")

	chains := rt.GetChainsByModel("model-b")
	if len(chains) != 0 {
		t.Errorf("expected 0 chains for non-matching model, got %d", len(chains))
	}
}
