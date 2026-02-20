package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/rag"
)

type RememberTool struct {
	workspace string
	rag       *rag.Store
}

func NewRememberTool(workspace string, ragStore *rag.Store) *RememberTool {
	return &RememberTool{
		workspace: workspace,
		rag:       ragStore,
	}
}

func (t *RememberTool) Name() string {
	return "remember"
}

func (t *RememberTool) Description() string {
	return "Store a fact or memory for long-term recall. ALWAYS use this tool to remember important information."
}

func (t *RememberTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The fact or memory to store",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Category (e.g. user_preference, project_detail, generic)",
			},
		},
		"required": []string{"content"},
	}
}

func (t *RememberTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("Missing content argument")
	}

	category, _ := args["category"].(string)
	if category == "" {
		category = "generic"
	}

	// 1. Write to MEMORY.md (Append)
	memoryPath := filepath.Join(t.workspace, "memory", "MEMORY.md")
	os.MkdirAll(filepath.Dir(memoryPath), 0755)

	entry := fmt.Sprintf("\n- [%s] (%s) %s", time.Now().Format("2006-01-02"), category, content)
	
	f, err := os.OpenFile(memoryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write to MEMORY.md: %v", err))
	}
	defer f.Close()
	
	if _, err := f.WriteString(entry); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write to MEMORY.md: %v", err))
	}

	// 2. Ingest into RAG
	if t.rag != nil {
		if err := t.rag.Ingest(ctx, content, "memory_tool"); err != nil {
			return ErrorResult(fmt.Sprintf("Saved to file but failed to ingest into RAG: %v", err))
		}
	}

	return SilentResult("Memory stored successfully.")
}
