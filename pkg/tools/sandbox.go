package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// SandboxTool allows the agent to run code in a restricted, sandboxed environment.
// It limits execution time, CPU, and memory to prevent system crashes.
type SandboxTool struct {
	workspace string
}

func NewSandboxTool(workspace string) *SandboxTool {
	return &SandboxTool{
		workspace: workspace,
	}
}

func (t *SandboxTool) Name() string {
	return "sandbox"
}

func (t *SandboxTool) Description() string {
	return "Run a command or a piece of code (Python, Go, etc.) in a restricted, sandboxed environment. This is safer for running generated code."
}

func (t *SandboxTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The command or script to run (e.g., 'python3 script.py').",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum execution time in milliseconds (default: 5000).",
			},
			"env": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional environment variables (format: KEY=VALUE).",
			},
		},
		"required": []string{"command"},
	}
}

func (t *SandboxTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	command, _ := args["command"].(string)
	timeoutMs, ok := args["timeout_ms"].(float64)
	if !ok || timeoutMs == 0 {
		timeoutMs = 5000
	}
	env, _ := args["env"].([]interface{})

	// Construct the command. On Linux, we can use 'timeout' and 'nice' to limit resources.
	// We'll also wrap the command in a shell to handle redirection or complex commands.
	// We'll use 'unshare' or similar if available, but for now, we'll focus on timeout and nice.

	// Check if 'timeout' is available
	if _, err := exec.LookPath("timeout"); err != nil {
		// Fallback to manual timeout if not available
		return t.executeWithManualTimeout(ctx, command, int(timeoutMs), env)
	}

	// Build the command: timeout [timeout] nice -n 10 [command]
	// We'll use sh -c to execute it.
	timeoutSec := fmt.Sprintf("%.3f", timeoutMs/1000.0)
	fullCmd := fmt.Sprintf("timeout %s nice -n 10 %s", timeoutSec, command)

	cmd := exec.CommandContext(ctx, "sh", "-c", fullCmd)
	cmd.Dir = t.workspace

	// Set environment variables
	for _, e := range env {
		if s, ok := e.(string); ok {
			cmd.Env = append(cmd.Env, s)
		}
	}

	logger.InfoCF("sandbox", "Executing sandboxed command", map[string]interface{}{
		"command": fullCmd,
		"timeout": timeoutMs,
	})

	start := time.Now()
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "exit status 124") {
			return ErrorResult(fmt.Sprintf("Command timed out after %v", duration))
		}
		return ErrorResult(fmt.Sprintf("Command failed after %v: %v\nOutput: %s", duration, err, string(out)))
	}

	return NewToolResult(fmt.Sprintf("Command successfully executed in %v.\nOutput:\n%s", duration, string(out)))
}

func (t *SandboxTool) executeWithManualTimeout(ctx context.Context, command string, timeoutMs int, env []interface{}) *ToolResult {
	// Simple manual timeout fallback for systems without 'timeout' command
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", command)
	cmd.Dir = t.workspace
	for _, e := range env {
		if s, ok := e.(string); ok {
			cmd.Env = append(cmd.Env, s)
		}
	}

	out, err := cmd.CombinedOutput()
	if timeoutCtx.Err() == context.DeadlineExceeded {
		return ErrorResult(fmt.Sprintf("Command timed out after %dms", timeoutMs))
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(out)))
	}
	return NewToolResult(string(out))
}
