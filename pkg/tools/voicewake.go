package tools

import (
	"context"
	"fmt"

	"github.com/ianclemence/ghost/pkg/logger"
)

// VoiceWakeTool allows the agent to control its "always-listening" state for wake words.
// Adapted from OpenClaw's voicewake.ts.
type VoiceWakeTool struct {
	onUpdate func(active bool)
}

func NewVoiceWakeTool(onUpdate func(active bool)) *VoiceWakeTool {
	return &VoiceWakeTool{
		onUpdate: onUpdate,
	}
}

func (t *VoiceWakeTool) Name() string {
	return "voice_wake"
}

func (t *VoiceWakeTool) Description() string {
	return "Enable or disable the 'Always-Listening' mode for wake words (e.g., 'Hey Ghost'). When active, Ghost will listen for your voice via the Pi's microphone."
}

func (t *VoiceWakeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"active": map[string]interface{}{
				"type":        "boolean",
				"description": "Set to true to enable voice wake word detection, false to disable.",
			},
		},
		"required": []string{"active"},
	}
}

func (t *VoiceWakeTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	active, _ := args["active"].(bool)

	if t.onUpdate != nil {
		t.onUpdate(active)
	}

	state := "disabled"
	if active {
		state = "enabled"
	}

	logger.InfoCF("voice_wake", "Voice wake word detection updated", map[string]interface{}{
		"active": active,
	})

	return SilentResult(fmt.Sprintf("Voice wake word detection %s.", state))
}
