package tools

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

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
	return "Check for, or install, a new verified release of Ghost. Set check=true to only report the installed and available version — read-only, changes nothing, safe to call whenever the owner asks \"is there an update?\". To actually update, pass confirm=true: it runs on its own, takes a few minutes, and Ghost is briefly unavailable while it restarts. Only update when the owner asked for it."
}

func (t *UpdateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"check": map[string]interface{}{
				"type":        "boolean",
				"description": "Only check the installed and available version. Changes nothing.",
			},
			"confirm": map[string]interface{}{
				"type":        "boolean",
				"description": "Must be true to install an update. Confirms the owner asked for it.",
			},
		},
	}
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (t *UpdateTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if runtime.GOOS == "windows" {
		return &ToolResult{
			ForLLM:  "Update tool is only supported on Linux/Raspberry Pi.",
			ForUser: "Update tool is only supported on Linux/Raspberry Pi.",
			IsError: true,
		}
	}

	// Read-only check: ask the installed CLI rather than duplicating release
	// resolution here. It changes nothing and is safe on the phone.
	if c, _ := args["check"].(bool); c {
		return t.check(ctx)
	}

	// The one updater is `ghost update` (release-based, verified). It runs as
	// its own systemd unit: this process is sandboxed read-only and is
	// restarted by the update itself.
	if c, _ := args["confirm"].(bool); !c {
		return ErrorResult("Updating restarts Ghost. Ask the owner to confirm, then call update with confirm=true. To only check for a new version, call update with check=true.")
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

// check runs `ghost update --check`, which resolves the release and prints the
// versions without touching the install.
func (t *UpdateTool) check(ctx context.Context) *ToolResult {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, updaterun.BinaryPath(), "update", "--check").CombinedOutput()
	text := strings.TrimSpace(ansiEscape.ReplaceAllString(string(out), ""))
	if text == "" {
		text = "the updater printed nothing"
	}
	msg := "Update check (read-only):\n" + text
	if err != nil {
		return &ToolResult{
			ForLLM:  msg + "\nThe check did not complete: " + err.Error() + ". Say what is known and do not install anything.",
			ForUser: msg,
			IsError: true,
		}
	}
	return &ToolResult{ForLLM: msg, ForUser: msg}
}
