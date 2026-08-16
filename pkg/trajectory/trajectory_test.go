package trajectory

import (
	"os"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestDefaultCompressConfig(t *testing.T) {
	cfg := DefaultCompressConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.MaxTrajectories != 500 {
		t.Errorf("default max should be 500, got %d", cfg.MaxTrajectories)
	}
	if cfg.MinTurnsToCompress != 2 {
		t.Errorf("default min turns should be 2, got %d", cfg.MinTurnsToCompress)
	}
}

func TestCompressor_CompressTurn_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompressConfig()
	cfg.Enabled = false
	c := NewCompressor(tmpDir, cfg)

	traj := c.CompressTurn("s1", "hello", "hi", nil, "model", "prov", nil, 100, 3)
	if traj != nil {
		t.Error("expected nil when disabled")
	}
}

func TestCompressor_CompressTurn_TooFewTurns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompressConfig()
	c := NewCompressor(tmpDir, cfg)

	traj := c.CompressTurn("s1", "hello", "hi", nil, "model", "prov", nil, 100, 1)
	if traj != nil {
		t.Error("expected nil for fewer turns than min")
	}
}

func TestCompressor_CompressTurn_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompressConfig()
	c := NewCompressor(tmpDir, cfg)

	messages := []providers.Message{
		{Role: "user", Content: "Search for Go tutorials"},
		{Role: "assistant", Content: "I found several Go tutorials.", ToolCalls: []providers.ToolCall{
			{ID: "1", Function: &providers.FunctionCall{Name: "web_search", Arguments: "{}"}},
		}},
		{Role: "tool", Content: "search results..."},
		{Role: "assistant", Content: "Here are the Go tutorials I found."},
	}

	traj := c.CompressTurn("s1", "Search for Go tutorials", "Here are the Go tutorials I found.",
		messages, "kimi-k2.5", "moonshot", []string{"web_search"}, 500, 4)

	if traj == nil {
		t.Fatal("expected trajectory")
	}
	if traj.SessionKey != "s1" {
		t.Errorf("expected session s1, got %s", traj.SessionKey)
	}
	if traj.TaskCategory != "search" {
		t.Errorf("expected category search, got %s", traj.TaskCategory)
	}
	if traj.Outcome != OutcomePartial {
		t.Errorf("expected outcome partial, got %s", traj.Outcome)
	}
	if traj.QualityScore <= 0 {
		t.Error("quality score should be positive")
	}
	if len(traj.Actions) == 0 {
		t.Error("should have actions")
	}
	if len(traj.ToolsUsed) != 1 || traj.ToolsUsed[0] != "web_search" {
		t.Errorf("expected tools_used [web_search], got %v", traj.ToolsUsed)
	}
}

func TestClassifyTask(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"search for Go docs", "search"},
		{"write a hello world program", "creation"},
		{"read the file config.json", "retrieval"},
		{"edit the main function", "modification"},
		{"explain how garbage collection works", "explanation"},
		{"delete the temp directory", "deletion"},
		{"run the tests", "execution"},
		{"analyze the performance", "analysis"},
		{"summarize this conversation", "summarization"},
		{"help me with this", "assistance"},
		{"hello there", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := classifyTask(tt.input)
			if got != tt.want {
				t.Errorf("classifyTask(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractTask(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"line1\nline2", "line1"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractTask(tt.input)
		if got != tt.want {
			t.Errorf("extractTask(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetermineOutcome(t *testing.T) {
	tests := []struct {
		response string
		tools    []string
		want     Outcome
	}{
		{"Successfully completed the task", nil, OutcomeSuccess},
		{"Error: something went wrong", nil, OutcomeFailure},
		{"I searched and found results", []string{"web_search"}, OutcomePartial},
		{"Done!", nil, OutcomeSuccess},
		{"", nil, OutcomeFailure},
	}

	for _, tt := range tests {
		got := determineOutcome(tt.response, tt.tools)
		if got != tt.want {
			t.Errorf("determineOutcome(%q, %v) = %q, want %q", tt.response, tt.tools, got, tt.want)
		}
	}
}

func TestCalculateQuality(t *testing.T) {
	tests := []struct {
		name   string
		traj   *Trajectory
		minQ   float64
		maxQ   float64
	}{
		{
			name: "success with few actions",
			traj: &Trajectory{
				Outcome:    OutcomeSuccess,
				Actions:    make([]Action, 3),
				ToolsUsed:  []string{"a", "b"},
			},
			minQ: 0.8,
			maxQ: 1.0,
		},
		{
			name: "failure with many actions",
			traj: &Trajectory{
				Outcome:    OutcomeFailure,
				Actions:    make([]Action, 15),
				ToolsUsed:  []string{"a"},
			},
			minQ: 0.0,
			maxQ: 0.3,
		},
		{
			name: "partial success",
			traj: &Trajectory{
				Outcome:    OutcomePartial,
				Actions:    make([]Action, 5),
				ToolsUsed:  []string{"a"},
			},
			minQ: 0.5,
			maxQ: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateQuality(tt.traj)
			if got < tt.minQ || got > tt.maxQ {
				t.Errorf("calculateQuality = %f, want [%f, %f]", got, tt.minQ, tt.maxQ)
			}
		})
	}
}

func TestCompressor_MaxTrajectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompressConfig()
	cfg.MaxTrajectories = 3
	c := NewCompressor(tmpDir, cfg)

	for i := 0; i < 5; i++ {
		c.CompressTurn("s", "hello", "response", nil, "m", "p", nil, 100, 3)
	}

	trajs := c.GetTrajectories()
	if len(trajs) > 3 {
		t.Errorf("expected at most 3 trajectories, got %d", len(trajs))
	}
}

func TestCompressor_GetByCategory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())

	c.CompressTurn("s", "search for Go", "found", nil, "m", "p", nil, 100, 3)
	c.CompressTurn("s", "write a program", "done", nil, "m", "p", nil, 100, 3)

	search := c.GetByCategory("search")
	if len(search) != 1 {
		t.Errorf("expected 1 search trajectory, got %d", len(search))
	}
}

func TestCompressor_GetByOutcome(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())

	c.CompressTurn("s", "hello", "Successfully done!", nil, "m", "p", nil, 100, 3)
	c.CompressTurn("s", "hello", "Error: failed", nil, "m", "p", nil, 100, 3)

	success := c.GetByOutcome(OutcomeSuccess)
	if len(success) != 1 {
		t.Errorf("expected 1 success, got %d", len(success))
	}
}

func TestCompressor_GetStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())

	// Empty stats
	stats := c.GetStats()
	if stats["total"] != 0 {
		t.Error("empty stats should have total 0")
	}

	// Add some trajectories
	c.CompressTurn("s", "search for something", "found it", nil, "m", "p", nil, 100, 3)
	c.CompressTurn("s", "write a program", "done", nil, "m", "p", nil, 200, 3)

	stats = c.GetStats()
	if stats["total"] != 2 {
		t.Errorf("expected total 2, got %v", stats["total"])
	}
}

func TestCompressor_GetTopTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())

	c.CompressTurn("s", "search", "found", nil, "m", "p", []string{"web_search", "read_file"}, 100, 3)
	c.CompressTurn("s", "search again", "found", nil, "m", "p", []string{"web_search"}, 100, 3)

	top := c.GetTopTools(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(top))
	}
	if top[0]["tool"] != "web_search" {
		t.Errorf("top tool should be web_search, got %v", top[0]["tool"])
	}
}

func TestCompressor_ToJSONL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())
	c.CompressTurn("s", "hello", "hi", nil, "m", "p", nil, 100, 3)

	jsonl := c.ToJSONL()
	if jsonl == "" {
		t.Error("JSONL should not be empty")
	}
	// Each line should be valid JSON
	lines := splitLines(jsonl)
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}

func TestCompressor_SaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())
	c.CompressTurn("s", "hello", "hi", nil, "m", "p", nil, 100, 3)

	err = c.Save()
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	c2 := NewCompressor(tmpDir, DefaultCompressConfig())
	err = c2.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	trajs := c2.GetTrajectories()
	if len(trajs) != 1 {
		t.Errorf("expected 1 trajectory after load, got %d", len(trajs))
	}
}

func TestCompressor_GetRecentTrajectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trajectory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	c := NewCompressor(tmpDir, DefaultCompressConfig())

	for i := 0; i < 5; i++ {
		c.CompressTurn("s", "hello", "hi", nil, "m", "p", nil, 100, 3)
	}

	recent := c.GetRecentTrajectories(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent, got %d", len(recent))
	}
}

func TestUniqueStrings(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	got := uniqueStrings(input)
	if len(got) != 3 {
		t.Errorf("expected 3 unique, got %d", len(got))
	}
}

func TestTruncateStr(t *testing.T) {
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
		got := truncateStr(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range splitByNewline(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
