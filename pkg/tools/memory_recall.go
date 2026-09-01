package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// MemoryResult is one memory-note search result, shaped so the agent (which owns
// the memory store) can build it without importing tools.
type MemoryResult struct {
	Path     string
	Excerpt  string
	Score    float64
	Modified int64
}

// SearchMemo returns relevant memory-note hits for a query. It is injected by
// the agent so this tool stays decoupled from the storage implementation.
type SearchMemo func(query string, limit int) []MemoryResult

// MemoryRecall searches the on-disk memory notes (daily notes, MEMORY.md,
// captures) for relevant past content. It is the targeted long-tail retrieval
// primitive: use it when you need something Ghost wrote earlier that isn't in
// the current context. Bounded and local — no external index.
type MemoryRecall struct {
	workspace string
	search    SearchMemo
}

func NewMemoryRecall(workspace string) *MemoryRecall {
	return &MemoryRecall{workspace: workspace}
}

// SetSearch wires the memory-note retrieval implementation.
func (t *MemoryRecall) SetSearch(fn SearchMemo) { t.search = fn }

func (t *MemoryRecall) Name() string { return "memory_recall" }

func (t *MemoryRecall) Description() string {
	return "Search Ghost's memory notes (daily notes, MEMORY.md, captures) for past content on a topic. Use when: you need something Ghost wrote earlier that isn't in the current context. Do NOT use for: a live/external fact (use web_search or a skill), or a past conversation (use session_search). Returns ranked excerpts with their note names."
}

func (t *MemoryRecall) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The topic/keywords to search for. Example: \"grandma's birthday\" or \"shopping list\".",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results (default 5, max 10).",
				"default":     5,
				"minimum":     1.0,
				"maximum":     10.0,
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemoryRecall) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required")
	}
	limit := 5
	if l, ok := args["limit"].(float64); ok && int(l) > 0 && int(l) <= 10 {
		limit = int(l)
	}
	if t.search == nil {
		return ErrorResult("memory recall unavailable")
	}
	hits := t.search(query, limit)
	if len(hits) == 0 {
		return NewToolResult("No memory notes matched that.")
	}
	var sb strings.Builder
	for i, h := range hits {
		name := filepath.Base(h.Path)
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "- %s (relevance: %s)\n  %s", name, relevanceLabel(h.Score), h.Excerpt)
	}
	return NewToolResult(sb.String())
}

func relevanceLabel(score float64) string {
	switch {
	case score >= 50:
		return "high"
	case score >= 10:
		return "medium"
	default:
		return "low"
	}
}
