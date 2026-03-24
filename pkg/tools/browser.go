package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
)

// BrowserTool uses the 'agent-browser' CLI (a Node.js CDP wrapper) to provide
// interactive, accessibility-tree-based browser control.
// This replaces the old screenshot-only headless browser with full navigation, clicking, and typing.
type BrowserTool struct {
	workspace string
	action    string // e.g. "navigate", "click", "type", "press", "snapshot"
}

func NewBrowserTool(workspace string, action string) *BrowserTool {
	return &BrowserTool{
		workspace: workspace,
		action:    action,
	}
}

func (t *BrowserTool) Name() string {
	return "browser_" + t.action
}

func (t *BrowserTool) Description() string {
	switch t.action {
	case "navigate":
		return "Navigate the browser to a specific URL. Returns the accessibility tree of the page so you can see elements."
	case "snapshot":
		return "Returns the current page accessibility tree (ARIA snapshot) with element reference IDs (like @e5) that you can use to interact with the page."
	case "click":
		return "Click on an element on the current page using its reference ID (e.g. '@e5'). Always use the exact reference string from the accessibility tree."
	case "type":
		return "Type text into an input field on the current page. Requires the element reference ID."
	case "press":
		return "Press a keyboard key (e.g. 'Enter', 'Tab', 'Escape', 'ArrowDown') on the current page."
	default:
		return "Interact with the browser."
	}
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	props := map[string]interface{}{}
	required := []string{}

	switch t.action {
	case "navigate":
		props["url"] = map[string]interface{}{
			"type":        "string",
			"description": "The URL to navigate to (must include http/https).",
		}
		required = []string{"url"}
	case "snapshot":
		// No parameters required
	case "click":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e5').",
		}
		required = []string{"ref"}
	case "type":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e5').",
		}
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "The text to type into the field.",
		}
		props["press_enter"] = map[string]interface{}{
			"type":        "boolean",
			"description": "Whether to press Enter after typing (default: false).",
		}
		required = []string{"ref", "text"}
	case "press":
		props["key"] = map[string]interface{}{
			"type":        "string",
			"description": "The key to press (e.g. 'Enter', 'Tab', 'Escape').",
		}
		required = []string{"key"}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// executeCLI runs the agent-browser CLI.
func (t *BrowserTool) executeCLI(ctx context.Context, action string, args ...string) *ToolResult {
	// Ensure temp directory exists for session tracking (agent-browser usually uses ~/.agent-browser)
	// but we'll let the CLI manage its own state for now.

	cmdArgs := append([]string{action}, args...)
	
	// Default to non-interactive json output
	cmdArgs = append(cmdArgs, "--json")

	logger.DebugCF("browser", "Executing agent-browser", map[string]interface{}{
		"action": action,
		"args":   args,
	})

	cmd := exec.CommandContext(ctx, "agent-browser", cmdArgs...)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// If command not found, give a helpful error
		pathErr, isPathErr := err.(*exec.Error)
		if isPathErr && pathErr.Err == exec.ErrNotFound {
			return ErrorResult("The 'agent-browser' command is not installed. Please install it with: npm install -g agent-browser")
		}

		return ErrorResult(fmt.Sprintf("Browser action '%s' failed: %v\nStderr: %s", action, err, stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())
	return UserResult(output)
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	switch t.action {
	case "navigate":
		url, _ := args["url"].(string)
		if url == "" {
			return ErrorResult("url is required")
		}
		return t.executeCLI(ctx, "navigate", url)

	case "snapshot":
		return t.executeCLI(ctx, "snapshot")

	case "click":
		ref, _ := args["ref"].(string)
		if ref == "" {
			return ErrorResult("ref is required")
		}
		return t.executeCLI(ctx, "click", ref)

	case "type":
		ref, _ := args["ref"].(string)
		text, _ := args["text"].(string)
		if ref == "" || text == "" {
			return ErrorResult("ref and text are required")
		}
		
		cliArgs := []string{ref, text}
		if pressEnter, ok := args["press_enter"].(bool); ok && pressEnter {
			cliArgs = append(cliArgs, "--enter")
		}
		return t.executeCLI(ctx, "type", cliArgs...)

	case "press":
		key, _ := args["key"].(string)
		if key == "" {
			return ErrorResult("key is required")
		}
		return t.executeCLI(ctx, "press", key)

	default:
		return ErrorResult(fmt.Sprintf("Unknown browser action: %s", t.action))
	}
}
