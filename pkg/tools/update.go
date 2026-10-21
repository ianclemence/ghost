package tools

import (
	"context"
	"runtime"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/updaterun"
)

type UpdateTool struct {
	workspace string
	channel   string
	chatID    string
}

// SetContext records which conversation asked, so the release that comes up
// after the update can say how it went to the same place.
func (t *UpdateTool) SetContext(channel, chatID string) {
	t.channel, t.chatID = channel, chatID
}

func NewUpdateTool(workspace string) *UpdateTool {
	return &UpdateTool{workspace: workspace}
}

func (t *UpdateTool) Name() string {
	return "update"
}

func (t *UpdateTool) Description() string {
	return "Update Ghost to the latest verified release, then restart it. The update runs on its own and takes a few minutes; Ghost is briefly unavailable while it restarts. Only call this when the owner asked to update. Pass confirm=true."
}

func (t *UpdateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"confirm": map[string]interface{}{
				"type":        "boolean",
				"description": "Must be true. Confirms the owner asked for the update.",
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

	// The one updater is `ghost update` (release-based, verified). It runs as
	// its own systemd unit: this process is sandboxed read-only and is
	// restarted by the update itself.
	if c, _ := args["confirm"].(bool); !c {
		return ErrorResult("Updating restarts Ghost. Ask the owner to confirm, then call update with confirm=true.")
	}
	// The update restarts this process, so leave a note for the release that
	// comes up: it tells this conversation whether the update took and whether
	// Ghost is healthy.
	_ = updaterun.WritePending(updaterun.Pending{From: RunningVersion(), Channel: t.channel, ChatID: t.chatID})
	if err := updaterun.StartDetachedUpdate(); err != nil {
		updaterun.ClearPending()
		return ErrorResult("Couldn't start the update: " + err.Error())
	}
	logger.InfoCF("tools", "Update started", map[string]interface{}{"unit": updaterun.UpdateUnit})

	msg := "Update started. Ghost will restart when it finishes, then check itself and report back here."
	return &ToolResult{
		ForLLM:  msg,
		ForUser: msg,
	}
}
