package tools

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/sipeed/picoclaw/pkg/logger"
)

type UpdateTool struct {
	workspace string
}

func NewUpdateTool(workspace string) *UpdateTool {
	return &UpdateTool{workspace: workspace}
}

func (t *UpdateTool) Name() string {
	return "update"
}

func (t *UpdateTool) Description() string {
	return "Updates the bot from git, rebuilds, and restarts. Usage: update"
}

func (t *UpdateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"confirm": map[string]interface{}{
				"type":        "boolean",
				"description": "Confirm update",
			},
		},
	}
}

func (t *UpdateTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if runtime.GOOS == "windows" {
		return &ToolResult{
			ForLLM:  "Update tool is only supported on Linux/Raspberry Pi.",
			ForUser: "Update tool is only supported on Linux/Raspberry Pi.",
			IsError: true,
		}
	}

	cmdStr := "git pull && make install && sudo systemctl restart ghost"
	logger.InfoCF("tools", "Executing update", map[string]interface{}{"cmd": cmdStr})

	go func() {
		cmd := exec.Command("bash", "-c", cmdStr)
		// cmd.Dir = t.workspace // Use CWD
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.ErrorCF("tools", "Update failed", map[string]interface{}{"error": err, "output": string(output)})
		}
	}()

	msg := "Update initiated. System will restart shortly if successful."
	return &ToolResult{
		ForLLM:  msg,
		ForUser: msg,
	}
}
