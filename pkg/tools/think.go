package tools

import (
	"context"
	"strings"
)

// maxThoughtLen bounds what a thought can add to the turn. It is a scratchpad,
// not a place to paste a page.
const maxThoughtLen = 2000

// ThinkTool is a scratchpad the model can write to in the middle of a turn. It
// fetches nothing and changes nothing; it exists so that after a tool result
// arrives, and before the next action, the model has a place to check what it
// just learned against the rules it is working under. Offered on deep turns
// only, where chains of dependent calls make a wrong step expensive.
type ThinkTool struct{}

func NewThinkTool() *ThinkTool { return &ThinkTool{} }

func (t *ThinkTool) Name() string { return "think" }

func (t *ThinkTool) Description() string {
	return "Stop and think before the next step. Use it after tool results arrive and before a step that is costly to get wrong: " +
		"list the rules that apply, check the facts you have against them, and plan the next calls. " +
		"It does nothing else: no lookup, no change, nothing sent. Skip it for a single simple call."
}

func (t *ThinkTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"thought": map[string]interface{}{
				"type":        "string",
				"description": "What you know so far, which rules apply, and what you will do next.",
			},
		},
		"required": []string{"thought"},
	}
}

func (t *ThinkTool) Execute(_ context.Context, args map[string]interface{}) *ToolResult {
	thought, _ := args["thought"].(string)
	if strings.TrimSpace(thought) == "" {
		return ErrorResult("Write the thought to record. Nothing was recorded.")
	}
	if len(thought) > maxThoughtLen {
		return SilentResult("Thought noted (kept short: the scratchpad holds about 2,000 characters). Continue.")
	}
	return SilentResult("Thought noted. Continue.")
}
