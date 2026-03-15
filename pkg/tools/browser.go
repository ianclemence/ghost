package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// BrowserTool allows the agent to navigate the web using a headless browser (Chromium).
// It can take screenshots, dump HTML, and interact with complex web apps.
type BrowserTool struct {
	workspace string
	restrict  bool
}

func NewBrowserTool(workspace string, restrict bool) *BrowserTool {
	return &BrowserTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) Description() string {
	return "Navigate a website using a headless browser. Use this to take screenshots of dashboards or extract content from complex web apps (like React/Vue) that 'web_fetch' cannot read."
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to visit.",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"screenshot", "html"},
				"description": "The action to perform (default: 'html').",
			},
			"wait_for": map[string]interface{}{
				"type":        "string",
				"description": "Optional CSS selector to wait for before performing the action.",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	url, _ := args["url"].(string)
	action, ok := args["action"].(string)
	if !ok || action == "" {
		action = "html"
	}
	waitFor, _ := args["wait_for"].(string)

	// Determine if we should use chromium or chrome
	browserCmd := "chromium-browser"
	if _, err := exec.LookPath(browserCmd); err != nil {
		browserCmd = "google-chrome"
		if _, err := exec.LookPath(browserCmd); err != nil {
			return ErrorResult("No headless browser (chromium-browser or google-chrome) found. Please install one on your Pi.")
		}
	}

	tempDir := filepath.Join(t.workspace, "tmp", "browser")
	os.MkdirAll(tempDir, 0755)

	if action == "screenshot" {
		outPath := filepath.Join(tempDir, fmt.Sprintf("screenshot_%d.png", time.Now().UnixNano()))
		// --headless --screenshot --window-size=1280,720 --disable-gpu
		cmd := exec.CommandContext(ctx, browserCmd, "--headless", "--screenshot="+outPath, "--window-size=1280,720", "--disable-gpu", "--no-sandbox", url)
		logger.InfoCF("browser", "Taking screenshot", map[string]interface{}{"url": url, "out": outPath})
		
		if out, err := cmd.CombinedOutput(); err != nil {
			return ErrorResult(fmt.Sprintf("Failed to take screenshot: %v\nOutput: %s", err, string(out)))
		}
		
		relPath, _ := filepath.Rel(t.workspace, outPath)
		return NewToolResult(fmt.Sprintf("Screenshot successfully saved to %s", relPath))
	}

	// Default: dump HTML
	// --headless --dump-dom --disable-gpu
	cmd := exec.CommandContext(ctx, browserCmd, "--headless", "--dump-dom", "--disable-gpu", "--no-sandbox", url)
	logger.InfoCF("browser", "Dumping DOM", map[string]interface{}{"url": url})

	out, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to dump DOM: %v\nOutput: %s", err, string(out)))
	}

	return NewToolResult(string(out))
}
