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
	// WriteScopes resolves the scope tags for NEW memories from a session
	// (e.g. ["context:work"]). A nil resolver or empty result stores a
	// global (shared) memory, matching legacy behavior.
	WriteScopes func(sessionKey string) []string
}

func NewRememberTool(workspace string, ragStore *rag.Store) *RememberTool {
	return &RememberTool{
		workspace: workspace,
		rag:       ragStore,
	}
}

// SetWriteScopes installs the session→scope resolver used to tag stored
// memories so other contexts cannot retrieve them.
func (t *RememberTool) SetWriteScopes(fn func(sessionKey string) []string) { t.WriteScopes = fn }

func (t *RememberTool) Name() string {
	return "remember"
}

func (t *RememberTool) Description() string {
	return "Store a durable fact about the user for long-term recall (name, preference, life goal, etc.) in Ghost\u2019s memory. Use when the user states a lasting fact or a request that starts with \"remember that I...\". Do NOT use for: transient task context. Returns confirmation."
}

func (t *RememberTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The fact to remember, stated as a sentence. Example: \"I prefer tea at noon\".",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Optional category: user_preference, project_detail, life_goal, generic.",
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

	// 2. Ingest into RAG (scope-tagged so foreign contexts can't recall it)
	if t.rag != nil {
		scope := ""
		if t.WriteScopes != nil {
			if sc := t.WriteScopes(SessionKeyFromContext(ctx)); len(sc) > 0 {
				scope = sc[0]
			}
		}
		if err := t.rag.IngestScoped(ctx, content, "memory_tool", scope); err != nil {
			return ErrorResult(fmt.Sprintf("Saved to file but failed to ingest into RAG: %v", err))
		}
	}

	return SilentResult("Memory stored successfully.")
}
