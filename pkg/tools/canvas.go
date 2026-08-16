package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CanvasTool allows the agent to present visual content (HTML/CSS/JS) to the mobile app.
// Adapted from OpenClaw's canvas skill.
type CanvasTool struct {
	workspace string
	onPresent func(html string)
}

func NewCanvasTool(workspace string, onPresent func(html string)) *CanvasTool {
	return &CanvasTool{
		workspace: workspace,
		onPresent: onPresent,
	}
}

func (t *CanvasTool) Name() string {
	return "canvas"
}

func (t *CanvasTool) Description() string {
	return "Display a visual dashboard, chart, or interactive UI using HTML/CSS/JS. The content will be rendered in the mobile app."
}

func (t *CanvasTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"html": map[string]interface{}{
				"type":        "string",
				"description": "The full HTML content to display. Can include inline <style> and <script> tags.",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Optional title for the canvas window.",
			},
		},
		"required": []string{"html"},
	}
}

func (t *CanvasTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	html, _ := args["html"].(string)
	title, _ := args["title"].(string)

	if html == "" {
		return ErrorResult("html content is required")
	}

	// In a real scenario, we might save this to a file in the workspace
	// so the bridge can serve it, or send it directly via WebSocket.
	if t.onPresent != nil {
		t.onPresent(html)
	}

	// Save to workspace for persistence/reference
	canvasDir := filepath.Join(t.workspace, "tmp", "canvas")
	os.MkdirAll(canvasDir, 0755)
	filePath := filepath.Join(canvasDir, "last_canvas.html")
	os.WriteFile(filePath, []byte(html), 0644)

	msg := "Canvas updated successfully."
	if title != "" {
		msg = fmt.Sprintf("Canvas '%s' updated successfully.", title)
	}

	return SilentResult(msg)
}
