// Package learninggraph implements a cross-session knowledge graph.
// The graph connects skills, memory chunks, and trajectory patterns into
// a queryable network that reveals how knowledge relates across sessions.
//
// Node types:
//   - skill: installed or created skills
//   - memory: memory chunks from MEMORY.md or daily notes
//   - trajectory: compressed trajectory patterns
//   - topic: extracted topic clusters
//
// Edge types:
//   - related: semantic similarity between nodes
//   - used_with: skills used together in trajectories
//   - derived_from: trajectory patterns derived from skill usage
//   - mentions: memory chunks mentioning skill names
package learninggraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// NodeType identifies the type of graph node.
type NodeType string

const (
	NodeTypeSkill      NodeType = "skill"
	NodeTypeMemory     NodeType = "memory"
	NodeTypeTrajectory NodeType = "trajectory"
	NodeTypeTopic      NodeType = "topic"
)

// EdgeType identifies the type of graph edge.
type EdgeType string

const (
	EdgeRelated  EdgeType = "related"
	EdgeUsedWith EdgeType = "used_with"
	EdgeDerived  EdgeType = "derived_from"
	EdgeMentions EdgeType = "mentions"
)

// Node represents a vertex in the learning graph.
type Node struct {
	ID        string            `json:"id"`
	Label     string            `json:"label"`
	Type      NodeType          `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Edge represents a connection between two nodes.
type Edge struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   EdgeType `json:"type"`
	Weight float64  `json:"weight"` // 0.0-1.0 strength of connection
}

// Cluster groups nodes by category.
type Cluster struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// Graph is the full learning graph.
type Graph struct {
	Nodes    []Node     `json:"nodes"`
	Edges    []Edge     `json:"edges"`
	Clusters []Cluster  `json:"clusters"`
	Stats    GraphStats `json:"stats"`
}

// GraphStats holds aggregate graph statistics.
type GraphStats struct {
	TotalNodes      int     `json:"total_nodes"`
	TotalEdges      int     `json:"total_edges"`
	EdgesPerNode    float64 `json:"edges_per_node"`
	IsolatedNodes   int     `json:"isolated_nodes"`
	SkillNodes      int     `json:"skill_nodes"`
	MemoryNodes     int     `json:"memory_nodes"`
	TrajectoryNodes int     `json:"trajectory_nodes"`
	TopicNodes      int     `json:"topic_nodes"`
}

// GraphConfig configures the learning graph.
type GraphConfig struct {
	Enabled         bool    `json:"enabled"`
	MinEdgeWeight   float64 `json:"min_edge_weight"`   // min weight to include edge
	MaxNodes        int     `json:"max_nodes"`         // max nodes to keep
	TopicKeywordMin int     `json:"topic_keyword_min"` // min keyword occurrences for topic
}

// DefaultGraphConfig returns sensible defaults.
func DefaultGraphConfig() GraphConfig {
	return GraphConfig{
		Enabled:         true,
		MinEdgeWeight:   0.3,
		MaxNodes:        1000,
		TopicKeywordMin: 2,
	}
}

// LearningGraph manages the cross-session knowledge graph.
type LearningGraph struct {
	config    GraphConfig
	workspace string
	nodes     map[string]*Node
	edges     []Edge
	mu        sync.RWMutex
}

// NewLearningGraph creates a new LearningGraph.
func NewLearningGraph(workspace string, config GraphConfig) *LearningGraph {
	return &LearningGraph{
		config:    config,
		workspace: workspace,
		nodes:     make(map[string]*Node),
		edges:     make([]Edge, 0),
	}
}

// AddSkillNode adds or updates a skill node in the graph.
func (lg *LearningGraph) AddSkillNode(name, category, source string, useCount int) {
	if !lg.config.Enabled {
		return
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()

	id := "skill:" + name
	existing, ok := lg.nodes[id]
	if ok {
		// Update metadata
		existing.Metadata["use_count"] = intToStr(useCount)
		existing.Metadata["source"] = source
		existing.Timestamp = time.Now()
		return
	}

	lg.nodes[id] = &Node{
		ID:        id,
		Label:     name,
		Type:      NodeTypeSkill,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"category":  category,
			"source":    source,
			"use_count": intToStr(useCount),
		},
	}
}

// AddMemoryNode adds a memory chunk node.
func (lg *LearningGraph) AddMemoryNode(title, source string, chunkIndex int) {
	if !lg.config.Enabled {
		return
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()

	id := "memory:" + source + ":" + intToStr(chunkIndex)
	lg.nodes[id] = &Node{
		ID:        id,
		Label:     title,
		Type:      NodeTypeMemory,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"source": source,
			"index":  intToStr(chunkIndex),
		},
	}
}

// AddTrajectoryNode adds a trajectory pattern node.
func (lg *LearningGraph) AddTrajectoryNode(taskCategory string, count int, outcome string) {
	if !lg.config.Enabled {
		return
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()

	id := "traj:" + taskCategory
	existing, ok := lg.nodes[id]
	if ok {
		existing.Metadata["count"] = intToStr(count)
		existing.Timestamp = time.Now()
		return
	}

	lg.nodes[id] = &Node{
		ID:        id,
		Label:     taskCategory,
		Type:      NodeTypeTrajectory,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"count":   intToStr(count),
			"outcome": outcome,
		},
	}
}

// AddTopicNode adds a topic cluster node.
func (lg *LearningGraph) AddTopicNode(topic string, frequency int) {
	if !lg.config.Enabled {
		return
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()

	id := "topic:" + topic
	existing, ok := lg.nodes[id]
	if ok {
		existing.Metadata["frequency"] = intToStr(frequency)
		existing.Timestamp = time.Now()
		return
	}

	lg.nodes[id] = &Node{
		ID:        id,
		Label:     topic,
		Type:      NodeTypeTopic,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"frequency": intToStr(frequency),
		},
	}
}

// AddEdge adds a weighted edge between two nodes.
func (lg *LearningGraph) AddEdge(source, target string, edgeType EdgeType, weight float64) {
	if !lg.config.Enabled {
		return
	}
	if weight < lg.config.MinEdgeWeight {
		return
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()

	// Check if edge already exists
	for i, e := range lg.edges {
		if e.Source == source && e.Target == target && e.Type == edgeType {
			// Update weight to max
			if weight > lg.edges[i].Weight {
				lg.edges[i].Weight = weight
			}
			return
		}
	}

	lg.edges = append(lg.edges, Edge{
		Source: source,
		Target: target,
		Type:   edgeType,
		Weight: weight,
	})
}

// BuildSkillMemoryEdges connects skills to memory chunks by keyword overlap.
func (lg *LearningGraph) BuildSkillMemoryEdges() {
	lg.mu.RLock()
	skills := make([]*Node, 0)
	memories := make([]*Node, 0)
	for _, n := range lg.nodes {
		switch n.Type {
		case NodeTypeSkill:
			skills = append(skills, n)
		case NodeTypeMemory:
			memories = append(memories, n)
		}
	}
	lg.mu.RUnlock()

	for _, skill := range skills {
		skillTokens := tokenize(skill.Label)
		for _, mem := range memories {
			memTokens := tokenize(mem.Label)
			weight := jaccardSimilarity(skillTokens, memTokens)
			if weight >= lg.config.MinEdgeWeight {
				lg.AddEdge(mem.ID, skill.ID, EdgeMentions, weight)
			}
		}
	}
}

// BuildSkillSkillEdges connects related skills.
func (lg *LearningGraph) BuildSkillSkillEdges() {
	lg.mu.RLock()
	skills := make([]*Node, 0)
	for _, n := range lg.nodes {
		if n.Type == NodeTypeSkill {
			skills = append(skills, n)
		}
	}
	lg.mu.RUnlock()

	// Sort by ID so edge direction between two skills is deterministic (nodes
	// were gathered from a map, whose iteration order is random — without a
	// stable order a "related" edge could point either way, making edges /
	// queries flaky).
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })

	for i := 0; i < len(skills); i++ {
		for j := i + 1; j < len(skills); j++ {
			tokensI := tokenize(skills[i].Label + " " + skills[i].Metadata["category"])
			tokensJ := tokenize(skills[j].Label + " " + skills[j].Metadata["category"])
			weight := jaccardSimilarity(tokensI, tokensJ)
			if weight >= lg.config.MinEdgeWeight {
				lg.AddEdge(skills[i].ID, skills[j].ID, EdgeRelated, weight)
			}
		}
	}
}

// BuildTrajectorySkillEdges connects trajectory patterns to related skills.
func (lg *LearningGraph) BuildTrajectorySkillEdges() {
	lg.mu.RLock()
	trajectories := make([]*Node, 0)
	skills := make([]*Node, 0)
	for _, n := range lg.nodes {
		switch n.Type {
		case NodeTypeTrajectory:
			trajectories = append(trajectories, n)
		case NodeTypeSkill:
			skills = append(skills, n)
		}
	}
	lg.mu.RUnlock()

	for _, traj := range trajectories {
		trajTokens := tokenize(traj.Label)
		for _, skill := range skills {
			skillTokens := tokenize(skill.Label)
			weight := jaccardSimilarity(trajTokens, skillTokens)
			if weight >= lg.config.MinEdgeWeight {
				lg.AddEdge(skill.ID, traj.ID, EdgeDerived, weight)
			}
		}
	}
}

// BuildAllEdges runs all edge-building algorithms.
func (lg *LearningGraph) BuildAllEdges() {
	lg.BuildSkillMemoryEdges()
	lg.BuildSkillSkillEdges()
	lg.BuildTrajectorySkillEdges()
}

// tokenize splits text into lowercase words >= 3 chars.
func tokenize(text string) map[string]bool {
	tokens := make(map[string]bool)
	words := strings.Fields(strings.ToLower(text))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()-")
		if len(w) >= 3 {
			tokens[w] = true
		}
	}
	return tokens
}

// jaccardSimilarity computes Jaccard index between two token sets.
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for t := range a {
		if b[t] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// GetNode returns a node by ID.
func (lg *LearningGraph) GetNode(id string) *Node {
	lg.mu.RLock()
	defer lg.mu.RUnlock()
	node, ok := lg.nodes[id]
	if !ok {
		return nil
	}
	copy := *node
	return &copy
}

// GetNodesByType returns all nodes of a given type.
func (lg *LearningGraph) GetNodesByType(nodeType NodeType) []Node {
	lg.mu.RLock()
	defer lg.mu.RUnlock()
	var result []Node
	for _, n := range lg.nodes {
		if n.Type == nodeType {
			result = append(result, *n)
		}
	}
	return result
}

// GetEdgesFrom returns all edges originating from a node.
func (lg *LearningGraph) GetEdgesFrom(nodeID string) []Edge {
	lg.mu.RLock()
	defer lg.mu.RUnlock()
	var result []Edge
	for _, e := range lg.edges {
		if e.Source == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// GetEdgesTo returns all edges targeting a node.
func (lg *LearningGraph) GetEdgesTo(nodeID string) []Edge {
	lg.mu.RLock()
	defer lg.mu.RUnlock()
	var result []Edge
	for _, e := range lg.edges {
		if e.Target == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// GetNeighbors returns all nodes connected to a given node.
func (lg *LearningGraph) GetNeighbors(nodeID string) []Node {
	lg.mu.RLock()
	defer lg.mu.RUnlock()

	neighborIDs := make(map[string]bool)
	for _, e := range lg.edges {
		if e.Source == nodeID {
			neighborIDs[e.Target] = true
		}
		if e.Target == nodeID {
			neighborIDs[e.Source] = true
		}
	}

	var result []Node
	for id := range neighborIDs {
		if n, ok := lg.nodes[id]; ok {
			result = append(result, *n)
		}
	}
	return result
}

// GetClusters groups nodes by type.
func (lg *LearningGraph) GetClusters() []Cluster {
	lg.mu.RLock()
	defer lg.mu.RUnlock()

	counts := make(map[NodeType]int)
	for _, n := range lg.nodes {
		counts[n.Type]++
	}

	var clusters []Cluster
	for nodeType, count := range counts {
		clusters = append(clusters, Cluster{
			Category: string(nodeType),
			Count:    count,
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Count > clusters[j].Count
	})
	return clusters
}

// BuildGraph constructs the full graph with stats.
func (lg *LearningGraph) BuildGraph() Graph {
	lg.BuildAllEdges()

	lg.mu.RLock()
	defer lg.mu.RUnlock()

	nodes := make([]Node, 0, len(lg.nodes))
	for _, n := range lg.nodes {
		nodes = append(nodes, *n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Label < nodes[j].Label
	})

	edges := make([]Edge, len(lg.edges))
	copy(edges, lg.edges)

	// Calculate stats
	linkedNodes := make(map[string]bool)
	for _, e := range edges {
		linkedNodes[e.Source] = true
		linkedNodes[e.Target] = true
	}

	stats := GraphStats{
		TotalNodes:    len(nodes),
		TotalEdges:    len(edges),
		IsolatedNodes: len(nodes) - len(linkedNodes),
	}
	if stats.TotalNodes > 0 {
		stats.EdgesPerNode = float64(stats.TotalEdges) / float64(stats.TotalNodes)
	}
	for _, n := range nodes {
		switch n.Type {
		case NodeTypeSkill:
			stats.SkillNodes++
		case NodeTypeMemory:
			stats.MemoryNodes++
		case NodeTypeTrajectory:
			stats.TrajectoryNodes++
		case NodeTypeTopic:
			stats.TopicNodes++
		}
	}

	return Graph{
		Nodes:    nodes,
		Edges:    edges,
		Clusters: lg.GetClusters(),
		Stats:    stats,
	}
}

// GetTopKeywords extracts the most frequent keywords across memory nodes.
func (lg *LearningGraph) GetTopKeywords(n int) []map[string]interface{} {
	lg.mu.RLock()
	defer lg.mu.RUnlock()

	freq := make(map[string]int)
	for _, node := range lg.nodes {
		if node.Type == NodeTypeMemory {
			tokens := tokenize(node.Label)
			for t := range tokens {
				freq[t]++
			}
		}
	}

	type kw struct {
		word  string
		count int
	}
	var sorted []kw
	for word, count := range freq {
		sorted = append(sorted, kw{word, count})
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
			"keyword": sorted[i].word,
			"count":   sorted[i].count,
		}
	}
	return result
}

// Save persists the graph to disk.
func (lg *LearningGraph) Save() error {
	stateDir := filepath.Join(lg.workspace, "state", "learninggraph")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	lg.mu.RLock()
	data, err := json.MarshalIndent(map[string]interface{}{
		"nodes": lg.nodes,
		"edges": lg.edges,
	}, "", "  ")
	lg.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0644)
}

// Load restores the graph from disk.
func (lg *LearningGraph) Load() error {
	path := filepath.Join(lg.workspace, "state", "learninggraph", "graph.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var raw struct {
		Nodes map[string]*Node `json:"nodes"`
		Edges []Edge           `json:"edges"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	lg.mu.Lock()
	defer lg.mu.Unlock()
	lg.nodes = raw.Nodes
	lg.edges = raw.Edges
	return nil
}

func intToStr(i int) string {
	return strings.TrimSpace(strings.Repeat("0", 0)) + func() string {
		if i == 0 {
			return "0"
		}
		result := ""
		n := i
		for n > 0 {
			result = string(rune('0'+n%10)) + result
			n /= 10
		}
		return result
	}()
}
