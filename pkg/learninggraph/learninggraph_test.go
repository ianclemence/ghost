package learninggraph

import (
	"os"
	"testing"
)

func TestDefaultGraphConfig(t *testing.T) {
	cfg := DefaultGraphConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.MinEdgeWeight != 0.3 {
		t.Errorf("default min edge weight should be 0.3, got %f", cfg.MinEdgeWeight)
	}
	if cfg.MaxNodes != 1000 {
		t.Errorf("default max nodes should be 1000, got %d", cfg.MaxNodes)
	}
}

func TestLearningGraph_DisabledNoOp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultGraphConfig()
	cfg.Enabled = false
	lg := NewLearningGraph(tmpDir, cfg)

	lg.AddSkillNode("weather", "utility", "workspace", 5)

	graph := lg.BuildGraph()
	if len(graph.Nodes) != 0 {
		t.Error("expected no nodes when disabled")
	}
}

func TestLearningGraph_AddSkillNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 10)
	lg.AddSkillNode("news", "info", "builtin", 5)

	skills := lg.GetNodesByType(NodeTypeSkill)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skill nodes, got %d", len(skills))
	}
}

func TestLearningGraph_AddSkillNode_Update(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddSkillNode("weather", "utility", "workspace", 10) // update

	skills := lg.GetNodesByType(NodeTypeSkill)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill node (updated), got %d", len(skills))
	}
	if skills[0].Metadata["use_count"] != "10" {
		t.Errorf("expected use_count 10, got %s", skills[0].Metadata["use_count"])
	}
}

func TestLearningGraph_AddMemoryNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddMemoryNode("User likes Go programming", "memory", 0)
	lg.AddMemoryNode("Prefers dark mode", "profile", 1)

	memories := lg.GetNodesByType(NodeTypeMemory)
	if len(memories) != 2 {
		t.Fatalf("expected 2 memory nodes, got %d", len(memories))
	}
}

func TestLearningGraph_AddTrajectoryNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddTrajectoryNode("search", 5, "success")
	lg.AddTrajectoryNode("creation", 3, "partial")

	trajs := lg.GetNodesByType(NodeTypeTrajectory)
	if len(trajs) != 2 {
		t.Fatalf("expected 2 trajectory nodes, got %d", len(trajs))
	}
}

func TestLearningGraph_AddTopicNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddTopicNode("golang", 5)
	lg.AddTopicNode("web development", 3)

	topics := lg.GetNodesByType(NodeTypeTopic)
	if len(topics) != 2 {
		t.Fatalf("expected 2 topic nodes, got %d", len(topics))
	}
}

func TestLearningGraph_AddEdge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddMemoryNode("Check weather daily", "memory", 0)

	lg.AddEdge("memory:memory:0", "skill:weather", EdgeMentions, 0.8)

	edges := lg.GetEdgesFrom("memory:memory:0")
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Weight != 0.8 {
		t.Errorf("expected weight 0.8, got %f", edges[0].Weight)
	}
}

func TestLearningGraph_AddEdge_BelowThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddEdge("a", "b", EdgeRelated, 0.1) // below default threshold 0.3

	edges := lg.GetEdgesFrom("a")
	if len(edges) != 0 {
		t.Error("edge below threshold should not be added")
	}
}

func TestLearningGraph_BuildSkillMemoryEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddMemoryNode("Check weather forecast", "memory", 0)
	lg.AddMemoryNode("Unrelated topic", "memory", 1)

	lg.BuildSkillMemoryEdges()

	// "weather" and "Check weather forecast" share "weather" keyword
	edges := lg.GetEdgesTo("skill:weather")
	if len(edges) == 0 {
		t.Error("expected edges from memory to weather skill")
	}
}

func TestLearningGraph_BuildSkillSkillEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddSkillNode("weather-alerts", "utility", "workspace", 3)

	lg.BuildSkillSkillEdges()

	edges := lg.GetEdgesFrom("skill:weather")
	found := false
	for _, e := range edges {
		if e.Target == "skill:weather-alerts" {
			found = true
		}
	}
	if !found {
		t.Error("expected edge between related weather skills")
	}
}

func TestLearningGraph_BuildAllEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddMemoryNode("Check weather daily", "memory", 0)
	lg.AddTrajectoryNode("search", 3, "success")

	lg.BuildAllEdges()

	graph := lg.BuildGraph()
	if graph.Stats.TotalEdges == 0 {
		t.Error("expected some edges after BuildAllEdges")
	}
}

func TestLearningGraph_GetNeighbors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddSkillNode("news", "info", "builtin", 3)
	lg.AddEdge("skill:weather", "skill:news", EdgeRelated, 0.5)

	neighbors := lg.GetNeighbors("skill:weather")
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
	}
	if neighbors[0].ID != "skill:news" {
		t.Errorf("expected neighbor skill:news, got %s", neighbors[0].ID)
	}
}

func TestLearningGraph_GetClusters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddSkillNode("news", "info", "builtin", 3)
	lg.AddMemoryNode("User preference", "memory", 0)

	clusters := lg.GetClusters()
	if len(clusters) < 2 {
		t.Errorf("expected at least 2 clusters, got %d", len(clusters))
	}
}

func TestLearningGraph_BuildGraph_Stats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddMemoryNode("Check weather", "memory", 0)
	lg.AddEdge("skill:weather", "memory:memory:0", EdgeMentions, 0.8)

	graph := lg.BuildGraph()

	if graph.Stats.TotalNodes != 2 {
		t.Errorf("expected 2 nodes, got %d", graph.Stats.TotalNodes)
	}
	if graph.Stats.TotalEdges < 1 {
		t.Errorf("expected at least 1 edge, got %d", graph.Stats.TotalEdges)
	}
	if graph.Stats.SkillNodes != 1 {
		t.Errorf("expected 1 skill node, got %d", graph.Stats.SkillNodes)
	}
	if graph.Stats.MemoryNodes != 1 {
		t.Errorf("expected 1 memory node, got %d", graph.Stats.MemoryNodes)
	}
}

func TestLearningGraph_GetTopKeywords(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddMemoryNode("golang programming language", "memory", 0)
	lg.AddMemoryNode("golang web development", "memory", 1)
	lg.AddMemoryNode("python data science", "memory", 2)

	top := lg.GetTopKeywords(3)
	if len(top) == 0 {
		t.Error("expected some keywords")
	}

	// "golang" should appear in 2 memory nodes
	found := false
	for _, kw := range top {
		if kw["keyword"] == "golang" && kw["count"].(int) >= 2 {
			found = true
		}
	}
	if !found {
		t.Error("expected 'golang' keyword with count >= 2")
	}
}

func TestLearningGraph_GetNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddSkillNode("weather", "utility", "workspace", 5)

	node := lg.GetNode("skill:weather")
	if node == nil {
		t.Fatal("expected to find skill:weather")
	}
	if node.Label != "weather" {
		t.Errorf("expected label 'weather', got %s", node.Label)
	}

	missing := lg.GetNode("nonexistent")
	if missing != nil {
		t.Error("expected nil for nonexistent node")
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello World! This is a test.")
	if !tokens["hello"] {
		t.Error("expected 'hello' token")
	}
	if !tokens["world"] {
		t.Error("expected 'world' token")
	}
	if tokens["is"] {
		t.Error("'is' should be filtered (< 3 chars)")
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]bool
		b    map[string]bool
		min  float64
		max  float64
	}{
		{
			name: "identical",
			a:    map[string]bool{"go": true, "web": true},
			b:    map[string]bool{"go": true, "web": true},
			min:  1.0,
			max:  1.0,
		},
		{
			name: "disjoint",
			a:    map[string]bool{"go": true},
			b:    map[string]bool{"python": true},
			min:  0.0,
			max:  0.0,
		},
		{
			name: "partial overlap",
			a:    map[string]bool{"go": true, "web": true},
			b:    map[string]bool{"go": true, "api": true},
			min:  0.3,
			max:  0.4,
		},
		{
			name: "both empty",
			a:    map[string]bool{},
			b:    map[string]bool{},
			min:  0.0,
			max:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardSimilarity(tt.a, tt.b)
			if got < tt.min || got > tt.max {
				t.Errorf("jaccardSimilarity = %f, want [%f, %f]", got, tt.min, tt.max)
			}
		})
	}
}

func TestLearningGraph_SaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())
	lg.AddSkillNode("weather", "utility", "workspace", 5)
	lg.AddMemoryNode("Check weather", "memory", 0)
	lg.AddEdge("skill:weather", "memory:memory:0", EdgeMentions, 0.8)

	err = lg.Save()
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	lg2 := NewLearningGraph(tmpDir, DefaultGraphConfig())
	err = lg2.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	graph := lg2.BuildGraph()
	if graph.Stats.TotalNodes != 2 {
		t.Errorf("expected 2 nodes after load, got %d", graph.Stats.TotalNodes)
	}
}

func TestLearningGraph_EdgeDedup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lg := NewLearningGraph(tmpDir, DefaultGraphConfig())

	lg.AddEdge("a", "b", EdgeRelated, 0.5)
	lg.AddEdge("a", "b", EdgeRelated, 0.8) // should update weight

	edges := lg.GetEdgesFrom("a")
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge (deduped), got %d", len(edges))
	}
	if edges[0].Weight != 0.8 {
		t.Errorf("expected weight updated to 0.8, got %f", edges[0].Weight)
	}
}

func TestLearningGraph_MaxNodes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lg-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultGraphConfig()
	cfg.MaxNodes = 3
	lg := NewLearningGraph(tmpDir, cfg)

	for i := 0; i < 5; i++ {
		lg.AddSkillNode("skill"+string(rune('a'+i)), "cat", "src", i)
	}

	// MaxNodes is advisory — all nodes are stored but BuildGraph
	// reports the count. The caller decides what to trim.
	skills := lg.GetNodesByType(NodeTypeSkill)
	if len(skills) != 5 {
		t.Errorf("expected 5 skills stored, got %d", len(skills))
	}
}
